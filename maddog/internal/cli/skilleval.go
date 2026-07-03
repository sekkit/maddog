package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"maddog/internal/boot"
	"maddog/internal/config"
	"maddog/internal/provider"
	"maddog/internal/skilleval"
)

type skillevalSummary struct {
	BundleID       string                  `json:"bundle_id"`
	BundleIDs      []string                `json:"bundle_ids,omitempty"`
	Bundles        int                     `json:"bundles"`
	CandidateHash  string                  `json:"candidate_hash"`
	Replay         skilleval.OutcomeInfo   `json:"replay"`
	Replays        []skilleval.OutcomeInfo `json:"replays,omitempty"`
	Score          skilleval.ScoreResult   `json:"score"`
	Scores         []skilleval.ScoreResult `json:"scores,omitempty"`
	GuardrailPass  bool                    `json:"guardrail_pass"`
	Guardrail      string                  `json:"guardrail"`
	DryRun         bool                    `json:"dry_run"`
	Mode           string                  `json:"mode"`
	Provider       string                  `json:"provider,omitempty"`
	ModelRef       string                  `json:"model_ref,omitempty"`
	PromotionGrade bool                    `json:"promotion_grade"`
	Persisted      bool                    `json:"persisted,omitempty"`
}

func skillevalCommand(args []string) int {
	if len(args) > 0 && args[0] == "list" {
		return skillevalListCommand(args[1:])
	}
	fs := flag.NewFlagSet("skilleval", flag.ContinueOnError)
	var bundlePaths multiValueFlag
	fs.Var(&bundlePaths, "bundle", "path to bundle v2 JSON; repeat for held-out evaluation")
	candidatePath := fs.String("candidate", "", "path to candidate JSON")
	modelRef := fs.String("model", "", "provider/model ref; defaults to default_model")
	jsonOut := fs.Bool("json", false, "print result as JSON")
	dryRun := fs.Bool("dry-run", false, "score without provider replay")
	storeDir := fs.String("store-dir", "", "candidate store directory to persist score and guardrail")
	minBundles := fs.Int("min-bundles", 5, "minimum bundles required by guardrail")
	minScore := fs.Float64("min-score", 0.7, "minimum replay score")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(bundlePaths) == 0 || *candidatePath == "" {
		fmt.Fprintln(os.Stderr, "skilleval requires --bundle and --candidate")
		return 2
	}
	candidate, err := loadSkillEvalCandidate(*candidatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var prov provider.Provider
	resolvedModelRef := strings.TrimSpace(*modelRef)
	if !*dryRun {
		prov, resolvedModelRef, err = resolveConfiguredSkillEvalProvider(*modelRef)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	result, err := skilleval.EvaluateCandidate(context.Background(), skilleval.EvaluationRequest{
		Candidate:   candidate,
		BundlePaths: []string(bundlePaths),
		Provider:    prov,
		ModelRef:    resolvedModelRef,
		DryRun:      *dryRun,
		MinBundles:  *minBundles,
		MinScore:    *minScore,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	persisted := false
	if strings.TrimSpace(*storeDir) != "" {
		if _, err := skilleval.NewCandidateStore(*storeDir).RecordEvaluationWithProvenance(candidate.Hash, result.Score, result.Guardrail, result.Provenance()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		persisted = true
	}
	summary := skillevalSummary{
		BundleID:       result.BundleIDs[0],
		BundleIDs:      result.BundleIDs,
		Bundles:        len(result.Bundles),
		CandidateHash:  candidate.Hash,
		Replay:         result.Replays[0],
		Replays:        result.Replays,
		Score:          result.Score,
		Scores:         result.Scores,
		GuardrailPass:  result.Guardrail.Pass,
		Guardrail:      result.Guardrail.Reason,
		DryRun:         *dryRun,
		Mode:           result.Mode,
		Provider:       result.Provider,
		ModelRef:       result.ModelRef,
		PromotionGrade: result.PromotionGrade,
		Persisted:      persisted,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(summary); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return boolExit(result.Guardrail.Pass)
	}
	fmt.Printf("bundles: %d\ncandidate: %s\nreplay: %s\nscore: %.2f\nrail: %s\n", summary.Bundles, summary.CandidateHash, summary.Replay.FinalAnswer, summary.Score.Score, summary.Guardrail)
	return boolExit(result.Guardrail.Pass)
}

type multiValueFlag []string

func (f *multiValueFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *multiValueFlag) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}

func loadSkillEvalCandidate(path string) (skilleval.Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return skilleval.Candidate{}, err
	}
	var c skilleval.Candidate
	if err := json.Unmarshal(data, &c); err != nil {
		return skilleval.Candidate{}, err
	}
	return c, nil
}

func resolveConfiguredSkillEvalProvider(modelRef string) (provider.Provider, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		ref = strings.TrimSpace(cfg.DefaultModel)
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, "", fmt.Errorf("model %q is not configured", ref)
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	return prov, ref, err
}

func skillevalListCommand(args []string) int {
	fs := flag.NewFlagSet("skilleval list", flag.ContinueOnError)
	dir := fs.String("dir", "", "candidate store directory")
	jsonOut := fs.Bool("json", false, "print candidates as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*dir) == "" {
		fmt.Fprintln(os.Stderr, "skilleval list requires --dir")
		return 2
	}
	candidates, err := loadSkillEvalCandidates(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(candidates); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	for _, c := range candidates {
		fmt.Printf("%s\t%s\t%s\t%s\n", c.Hash, c.Status, c.Skill.Name, c.GuardrailReason)
	}
	return 0
}

func loadSkillEvalCandidates(dir string) ([]skilleval.Candidate, error) {
	files, err := filepath.Glob(filepath.Join(dir, "candidates", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]skilleval.Candidate, 0, len(files))
	for _, path := range files {
		c, err := loadSkillEvalCandidate(path)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func boolExit(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
