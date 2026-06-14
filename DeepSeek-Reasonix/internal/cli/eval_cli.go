package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	replayeval "reasonix/internal/eval"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func evalCommand(args []string) int {
	if len(args) == 0 {
		evalUsage()
		return 2
	}
	switch args[0] {
	case "list":
		return evalListCommand(args[1:])
	case "guard":
		return evalGuardCommand(args[1:])
	default:
		evalUsage()
		return 2
	}
}

func evalListCommand(args []string) int {
	fs := flag.NewFlagSet("eval list", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory containing replay bundle JSON files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	bundles, err := loadReplayBundles(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(bundles) == 0 {
		fmt.Println("no replay bundles found")
		return 0
	}
	for _, b := range bundles {
		status := "fail"
		if b.bundle.Outcome.Success && b.bundle.Outcome.GoalMet {
			status = "pass"
		}
		fmt.Printf("%s\t%s\tturns=%d\terrors=%d\t%s\n",
			status, b.bundle.SessionID, b.bundle.Outcome.TotalTurns, b.bundle.Outcome.ToolErrors, displayPath(b.path))
	}
	return 0
}

func evalGuardCommand(args []string) int {
	fs := flag.NewFlagSet("eval guard", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory containing replay bundle JSON files")
	minBundles := fs.Int("min-bundles", 5, "minimum bundle count required to pass")
	minScore := fs.Float64("min-score", 0.7, "minimum score required per bundle")
	model := fs.String("model", "", "optional configured model for replay/scoring")
	promotePath := fs.String("promote-skill", "", "candidate skill markdown to promote after guardrail passes")
	scope := fs.String("scope", "project", "promotion scope: project or global")
	projectRoot := fs.String("project-root", ".", "project root for project-scope promotion")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	bundles, err := loadReplayBundles(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	rawBundles := make([]replayeval.ReplayBundle, len(bundles))
	oldResults := make([]replayeval.OutcomeInfo, len(bundles))
	newResults := make([]replayeval.OutcomeInfo, len(bundles))
	scores := make([]float64, len(bundles))
	for i, loaded := range bundles {
		rawBundles[i] = *loaded.bundle
		oldResults[i] = loaded.bundle.Outcome
		newResults[i] = loaded.bundle.Outcome
		scores[i] = 1
	}
	if strings.TrimSpace(*model) != "" {
		if err := replayWithModel(context.Background(), *model, rawBundles, newResults, scores); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
	}
	result := replayeval.CheckGuardrail(rawBundles, oldResults, newResults, scores, replayeval.GuardrailConfig{
		MinBundles: *minBundles,
		MinScore:   *minScore,
	})
	fmt.Println(result.Reason)
	if !result.Pass {
		return 1
	}
	if strings.TrimSpace(*promotePath) == "" {
		return 0
	}
	path, err := promoteCandidateSkill(*promotePath, *scope, *projectRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("promoted skill:", displayPath(path))
	return 0
}

type loadedBundle struct {
	path   string
	bundle *replayeval.ReplayBundle
}

func loadReplayBundles(dir string) ([]loadedBundle, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = config.SessionDir()
	}
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]loadedBundle, 0, len(paths))
	for _, path := range paths {
		b, err := replayeval.LoadBundle(path)
		if err != nil {
			continue
		}
		out = append(out, loadedBundle{path: path, bundle: b})
	}
	return out, nil
}

func replayWithModel(ctx context.Context, model string, bundles []replayeval.ReplayBundle, newResults []replayeval.OutcomeInfo, scores []float64) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	entry, ok := cfg.ResolveModel(model)
	if !ok {
		return fmt.Errorf("unknown model %q", model)
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		return err
	}
	runner := replayeval.ReplayRunner{Provider: prov, Registry: tool.NewRegistry(), MaxSteps: 4}
	for i := range bundles {
		name := strings.TrimSpace(bundles[i].SkillName)
		if name == "" {
			name = "replay"
		}
		sk := skill.Skill{Name: name, Description: "Replay candidate", Body: "Replay the captured task faithfully and produce the best final answer."}
		out, err := runner.Run(ctx, bundles[i], sk)
		if err != nil {
			return fmt.Errorf("replay %s: %w", bundles[i].SessionID, err)
		}
		newResults[i] = out
		score, err := replayeval.Score(ctx, prov, bundles[i].Outcome, out)
		if err != nil {
			return fmt.Errorf("score %s: %w", bundles[i].SessionID, err)
		}
		scores[i] = score.Score
	}
	return nil
}

func promoteCandidateSkill(path, scopeName, projectRoot string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	scope := skill.ScopeProject
	switch strings.ToLower(strings.TrimSpace(scopeName)) {
	case "", "project":
		scope = skill.ScopeProject
	case "global":
		scope = skill.ScopeGlobal
	default:
		return "", fmt.Errorf("unknown scope %q", scopeName)
	}
	sk, err := skill.ParseMarkdown(string(data), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), scope)
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	store := skill.New(skill.Options{HomeDir: home, ProjectRoot: projectRoot})
	out, _, err := replayeval.Promote(store, sk, scope)
	return out, err
}

func evalUsage() {
	fmt.Print(`Usage:
  maddog eval list  --dir <replay-dir>
  maddog eval guard --dir <replay-dir> [--min-bundles N] [--min-score F] [--model NAME] [--promote-skill skill.md] [--scope project|global]
`)
}
