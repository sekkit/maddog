package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"maddog/internal/boot"
	"maddog/internal/config"
	"maddog/internal/evalbench"
	"maddog/internal/skill"
	"maddog/internal/skillopt"
)

func skilloptCommand(args []string) int {
	if len(args) == 0 {
		skilloptUsage()
		return 2
	}
	switch args[0] {
	case "optimize":
		return skilloptOptimizeCommand(args[1:])
	case "status":
		return skilloptStatusCommand(args[1:])
	case "resume":
		return skilloptResumeCommand(args[1:])
	case "cancel":
		return skilloptCancelCommand(args[1:])
	case "promote":
		return skilloptPromoteCommand(args[1:])
	case "rollback":
		return skilloptRollbackCommand(args[1:])
	case "cleanup":
		return skilloptCleanupCommand(args[1:])
	default:
		skilloptUsage()
		return 2
	}
}

func skilloptOptimizeCommand(args []string) int {
	fs := flag.NewFlagSet("skillopt optimize", flag.ContinueOnError)
	skillName := fs.String("skill", "", "project skill name")
	manifest := fs.String("manifest", "", "explicit train/validation/test JSON or TOML manifest")
	suite := fs.String("suite", "", "evalbench suite root containing tasks/<id>")
	runID := fs.String("run-id", "", "durable run id (generated when empty)")
	storeDir := fs.String("store-dir", "", "checkpoint directory (default .maddog/skillopt/runs)")
	binary := fs.String("binary", "", "maddog binary used for isolated rollouts")
	model := fs.String("model", "", "rollout model override")
	proposerModel := fs.String("proposer-model", "", "optimizer model override")
	rounds := fs.Int("rounds", 0, "optimization rounds override")
	batchSize := fs.Int("batch-size", 0, "training batch size override")
	maxCalls := fs.Int("max-calls", 0, "total rollout + proposal call budget")
	maxInput := fs.Int64("max-input-tokens", 0, "input-token budget")
	maxOutput := fs.Int64("max-output-tokens", 0, "output-token budget")
	maxCost := fs.Float64("max-cost", 0, "estimated provider cost budget")
	jsonOut := fs.Bool("json", false, "print run summary as JSON")
	keepWorkspaces := fs.Bool("keep-workspaces", false, "retain disposable case workspaces for diagnosis")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*skillName) == "" || strings.TrimSpace(*manifest) == "" || strings.TrimSpace(*suite) == "" {
		fmt.Fprintln(os.Stderr, "skillopt optimize requires --skill, --manifest, and --suite")
		return 2
	}
	root, cfg, optimization, err := loadSkillOptProject()
	if err != nil {
		return skilloptError(err)
	}
	if !optimization.Enabled {
		return skilloptError(fmt.Errorf("skill optimization is disabled; set [skills.optimization].enabled = true in the project maddog.toml"))
	}
	dataset, err := skillopt.LoadDataset(*manifest)
	if err != nil {
		return skilloptError(err)
	}
	tasks, err := evalbench.LoadSuite(*suite)
	if err != nil {
		return skilloptError(err)
	}
	if err := validateSkillOptTasks(dataset, tasks); err != nil {
		return skilloptError(err)
	}
	skillStore := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	initial, ok := skillStore.Read(strings.TrimSpace(*skillName))
	if !ok {
		return skilloptError(fmt.Errorf("project skill %q was not found", *skillName))
	}
	engineConfig := skilloptConfig(optimization, *model, *proposerModel)
	if *rounds > 0 {
		engineConfig.MaxRounds = *rounds
	}
	if *batchSize > 0 {
		engineConfig.TrainBatchSize = *batchSize
	}
	if *maxCalls > 0 {
		engineConfig.Budget.MaxCalls = *maxCalls
	}
	if *maxInput > 0 {
		engineConfig.Budget.MaxInputTokens = *maxInput
	}
	if *maxOutput > 0 {
		engineConfig.Budget.MaxOutputTokens = *maxOutput
	}
	if *maxCost > 0 {
		engineConfig.Budget.MaxAmount = *maxCost
	}
	id := strings.TrimSpace(*runID)
	if id == "" {
		id = generatedSkillOptRunID(initial.Name)
	}
	dir := resolveSkillOptStoreDir(root, *storeDir)
	store := skillopt.NewJSONRunStore(dir)
	engine, err := buildSkillOptEngine(root, cfg, engineConfig, tasks, *binary, store, *keepWorkspaces)
	if err != nil {
		return skilloptError(err)
	}
	run, err := engine.Start(context.Background(), skillopt.Request{RunID: id, Dataset: dataset, Skill: initial, Config: engineConfig})
	if err != nil && run == nil {
		return skilloptError(err)
	}
	printSkillOptRun(run, *jsonOut)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func skilloptStatusCommand(args []string) int {
	fs, runID, storeDir, jsonOut := skilloptRunFlags("skillopt status")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runID) == "" {
		fmt.Fprintln(os.Stderr, "skillopt status requires --run")
		return 2
	}
	root, _ := os.Getwd()
	run, err := skillopt.NewJSONRunStore(resolveSkillOptStoreDir(root, *storeDir)).Load(context.Background(), *runID)
	if err != nil {
		return skilloptError(err)
	}
	printSkillOptRun(run, *jsonOut)
	return 0
}

func skilloptResumeCommand(args []string) int {
	fs := flag.NewFlagSet("skillopt resume", flag.ContinueOnError)
	runID := fs.String("run", "", "run id")
	storeDir := fs.String("store-dir", "", "checkpoint directory")
	suite := fs.String("suite", "", "evalbench suite root")
	binary := fs.String("binary", "", "maddog binary used for rollouts")
	jsonOut := fs.Bool("json", false, "print run summary as JSON")
	keepWorkspaces := fs.Bool("keep-workspaces", false, "retain case workspaces")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runID) == "" || strings.TrimSpace(*suite) == "" {
		fmt.Fprintln(os.Stderr, "skillopt resume requires --run and --suite")
		return 2
	}
	root, cfg, optimization, err := loadSkillOptProject()
	if err != nil {
		return skilloptError(err)
	}
	if !optimization.Enabled {
		return skilloptError(fmt.Errorf("skill optimization is disabled in project config"))
	}
	tasks, err := evalbench.LoadSuite(*suite)
	if err != nil {
		return skilloptError(err)
	}
	store := skillopt.NewJSONRunStore(resolveSkillOptStoreDir(root, *storeDir))
	checkpoint, err := store.Load(context.Background(), *runID)
	if err != nil {
		return skilloptError(err)
	}
	if err := validateSkillOptTasks(checkpoint.Dataset, tasks); err != nil {
		return skilloptError(err)
	}
	engine, err := buildSkillOptEngine(root, cfg, checkpoint.Config, tasks, *binary, store, *keepWorkspaces)
	if err != nil {
		return skilloptError(err)
	}
	run, err := engine.Resume(context.Background(), *runID)
	if run != nil {
		printSkillOptRun(run, *jsonOut)
	}
	if err != nil {
		return skilloptError(err)
	}
	return 0
}

func skilloptCancelCommand(args []string) int {
	fs := flag.NewFlagSet("skillopt cancel", flag.ContinueOnError)
	runID := fs.String("run", "", "run id")
	storeDir := fs.String("store-dir", "", "checkpoint directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runID) == "" {
		fmt.Fprintln(os.Stderr, "skillopt cancel requires --run")
		return 2
	}
	root, _ := os.Getwd()
	if err := skillopt.NewJSONRunStore(resolveSkillOptStoreDir(root, *storeDir)).Cancel(context.Background(), *runID); err != nil {
		return skilloptError(err)
	}
	fmt.Printf("cancel requested: %s\n", *runID)
	return 0
}

func skilloptPromoteCommand(args []string) int {
	fs := flag.NewFlagSet("skillopt promote", flag.ContinueOnError)
	runID := fs.String("run", "", "completed run id")
	storeDir := fs.String("store-dir", "", "checkpoint directory")
	yes := fs.Bool("yes", false, "approve deployment of the best revision")
	jsonOut := fs.Bool("json", false, "print run summary as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runID) == "" {
		fmt.Fprintln(os.Stderr, "skillopt promote requires --run")
		return 2
	}
	root, _, optimization, err := loadSkillOptProject()
	if err != nil {
		return skilloptError(err)
	}
	if optimization.RequireApproval != nil && *optimization.RequireApproval && !*yes {
		return skilloptError(fmt.Errorf("promotion requires explicit approval; rerun with --yes after reviewing status"))
	}
	runs := skillopt.NewJSONRunStore(resolveSkillOptStoreDir(root, *storeDir))
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	run, err := skillopt.PromoteBest(context.Background(), runs, *runID, skills, skill.ScopeProject)
	if err != nil {
		return skilloptError(err)
	}
	printSkillOptRun(run, *jsonOut)
	return 0
}

func skilloptRollbackCommand(args []string) int {
	fs := flag.NewFlagSet("skillopt rollback", flag.ContinueOnError)
	runID := fs.String("run", "", "promoted run id")
	storeDir := fs.String("store-dir", "", "checkpoint directory")
	reason := fs.String("reason", "manual rollback", "audit reason")
	yes := fs.Bool("yes", false, "confirm rollback")
	jsonOut := fs.Bool("json", false, "print run summary as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runID) == "" || !*yes {
		fmt.Fprintln(os.Stderr, "skillopt rollback requires --run and --yes")
		return 2
	}
	root, _ := os.Getwd()
	runs := skillopt.NewJSONRunStore(resolveSkillOptStoreDir(root, *storeDir))
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	run, err := skillopt.RollbackPromotion(context.Background(), runs, *runID, skills, *reason)
	if err != nil {
		return skilloptError(err)
	}
	printSkillOptRun(run, *jsonOut)
	return 0
}

func skilloptCleanupCommand(args []string) int {
	fs := flag.NewFlagSet("skillopt cleanup", flag.ContinueOnError)
	storeDir := fs.String("store-dir", "", "checkpoint directory")
	olderThan := fs.Duration("older-than", 0, "delete terminal runs older than this duration")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, cfg, optimization, err := loadSkillOptProject()
	if err != nil {
		return skilloptError(err)
	}
	_ = cfg
	retention := *olderThan
	if retention <= 0 {
		retention = time.Duration(optimization.RetentionDays) * 24 * time.Hour
	}
	removed, err := skillopt.NewJSONRunStore(resolveSkillOptStoreDir(root, *storeDir)).Cleanup(context.Background(), time.Now().Add(-retention))
	if err != nil {
		return skilloptError(err)
	}
	for _, id := range removed {
		fmt.Println(id)
	}
	return 0
}

func skilloptRunFlags(name string) (*flag.FlagSet, *string, *string, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs, fs.String("run", "", "run id"), fs.String("store-dir", "", "checkpoint directory"), fs.Bool("json", false, "print run summary as JSON")
}

func loadSkillOptProject() (string, *config.Config, config.SkillOptimizationConfig, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", nil, config.SkillOptimizationConfig{}, err
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return "", nil, config.SkillOptimizationConfig{}, err
	}
	return root, cfg, cfg.EffectiveSkillOptimizationConfig(), nil
}

func skilloptConfig(value config.SkillOptimizationConfig, rolloutOverride, proposerOverride string) skillopt.Config {
	out := skillopt.DefaultConfig()
	out.MaxRounds = value.Rounds
	out.TrainBatchSize = value.BatchSize
	out.MaxConcurrency = value.MaxConcurrency
	out.MinDelta = value.MinDelta
	out.Deadband = value.Deadband
	out.RolloutModelRef = firstCLIValue(rolloutOverride, value.Model)
	out.ProposerModelRef = firstCLIValue(proposerOverride, value.ProposerModel, out.RolloutModelRef)
	out.Budget = skillopt.Budget{
		MaxCalls: value.MaxCalls, MaxInputTokens: value.MaxInputTokens,
		MaxOutputTokens: value.MaxOutputTokens, MaxAmount: value.MaxCost,
	}
	return out
}

func buildSkillOptEngine(root string, cfg *config.Config, engineConfig skillopt.Config, tasks []evalbench.Task, binary string, store skillopt.RunStore, keepWorkspaces bool) (*skillopt.Engine, error) {
	rolloutRef := firstCLIValue(engineConfig.RolloutModelRef, cfg.DefaultModel)
	proposerRef := firstCLIValue(engineConfig.ProposerModelRef, rolloutRef)
	rolloutEntry, ok := cfg.ResolveModel(rolloutRef)
	if !ok {
		return nil, fmt.Errorf("rollout model %q is not configured", rolloutRef)
	}
	entry, ok := cfg.ResolveModel(proposerRef)
	if !ok {
		return nil, fmt.Errorf("optimizer model %q is not configured", proposerRef)
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(binary) == "" {
		binary, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	runner := skillopt.NewEvalbenchExecutor(tasks, binary, rolloutRef)
	runner.KeepWorkspace = keepWorkspaces
	// The disposable target process needs only the selected rollout provider's
	// credential. Forward it explicitly instead of relying on nested process
	// inheritance, which is inconsistent on Windows when environment keys are
	// resolved through the credential layer.
	if name, value := strings.TrimSpace(rolloutEntry.AuthEnvName()), rolloutEntry.AuthToken(); name != "" && value != "" {
		runner.Environment = append(runner.Environment, name+"="+value)
	}
	if name, value := strings.TrimSpace(rolloutEntry.IdentityEnv), rolloutEntry.IdentityToken(); name != "" && value != "" {
		runner.Environment = append(runner.Environment, name+"="+value)
	}
	projectConfig := config.ProjectConfigPathForRoot(root)
	if info, statErr := os.Stat(projectConfig); statErr == nil && !info.IsDir() {
		runner.ProjectConfig = projectConfig
	}
	return skillopt.NewEngine(skillopt.EngineOptions{
		Store: store, Rollout: runner,
		Proposer: skillopt.ProviderProposer{Provider: prov, Pricing: entry.Price, ModelRef: proposerRef},
	}), nil
}

func validateSkillOptTasks(dataset skillopt.Dataset, tasks []evalbench.Task) error {
	loaded := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		loaded[task.ID] = true
	}
	for split, cases := range map[string][]skillopt.Case{"train": dataset.Train, "validation": dataset.Validation, "test": dataset.Test} {
		for _, item := range cases {
			taskID := strings.TrimSpace(item.Metadata["task_id"])
			if taskID == "" {
				taskID = item.ID
			}
			if !loaded[taskID] {
				return fmt.Errorf("%s case %q references missing suite task %q", split, item.ID, taskID)
			}
		}
	}
	return nil
}

type skilloptRunSummary struct {
	ID        string                       `json:"id"`
	Status    skillopt.RunStatus           `json:"status"`
	NextRound int                          `json:"next_round"`
	MaxRounds int                          `json:"max_rounds"`
	Baseline  string                       `json:"baseline_revision"`
	Current   string                       `json:"current_revision"`
	Best      string                       `json:"best_revision"`
	Rounds    []skillopt.RoundRecord       `json:"rounds"`
	Rejected  []skillopt.RejectedCandidate `json:"rejected"`
	Usage     skillopt.Usage               `json:"usage"`
	Test      skillopt.TestRecord          `json:"test"`
	Promotion *skillopt.PromotionRecord    `json:"promotion,omitempty"`
	LastError string                       `json:"last_error,omitempty"`
}

func printSkillOptRun(run *skillopt.Run, jsonOut bool) {
	if run == nil {
		return
	}
	summary := skilloptRunSummary{
		ID: run.ID, Status: run.Status, NextRound: run.NextRound, MaxRounds: run.Config.MaxRounds,
		Baseline: run.BaselineRevisionID, Current: run.CurrentRevisionID, Best: run.BestRevisionID,
		Rounds: run.Rounds, Rejected: run.Rejected, Usage: run.Usage, Test: run.Test,
		Promotion: run.Promotion, LastError: run.LastError,
	}
	if jsonOut {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(summary)
		return
	}
	fmt.Printf("run: %s\nstatus: %s\nround: %d/%d\nbest: %s\ncalls: %d\n", summary.ID, summary.Status, summary.NextRound, summary.MaxRounds, summary.Best, summary.Usage.Calls)
	for i := len(summary.Rounds) - 1; i >= 0; i-- {
		decision := summary.Rounds[i].Decision
		if decision == nil {
			continue
		}
		fmt.Printf("gate: round=%d accepted=%t hard_delta=%g soft_delta=%g reason=%s\n", summary.Rounds[i].Number, decision.Accepted, decision.HardDelta, decision.SoftDelta, decision.Reason)
		break
	}
	if summary.LastError != "" {
		fmt.Printf("error: %s\n", summary.LastError)
	}
	if summary.Promotion != nil && !summary.Promotion.PromotedAt.IsZero() {
		fmt.Printf("promoted: %s\n", summary.Promotion.Path)
	}
}

func resolveSkillOptStoreDir(root, override string) string {
	if strings.TrimSpace(override) != "" {
		if filepath.IsAbs(override) {
			return filepath.Clean(override)
		}
		return filepath.Join(root, override)
	}
	return filepath.Join(root, config.ProjectConventionDir, "skillopt", "runs")
}

func generatedSkillOptRunID(name string) string {
	return fmt.Sprintf("%s-%s", strings.TrimSpace(name), time.Now().UTC().Format("20060102T150405Z"))
}

func firstCLIValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func skilloptError(err error) int {
	if err != nil && !errors.Is(err, skillopt.ErrCanceled) && !errors.Is(err, skillopt.ErrBudgetExceeded) {
		fmt.Fprintln(os.Stderr, err)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return 1
}

func skilloptUsage() {
	fmt.Fprintln(os.Stderr, "usage: maddog skillopt <optimize|status|resume|cancel|promote|rollback|cleanup> [flags]")
}
