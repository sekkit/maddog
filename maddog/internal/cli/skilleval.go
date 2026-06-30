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
	BundleID      string                `json:"bundle_id"`
	CandidateHash string                `json:"candidate_hash"`
	Replay        skilleval.OutcomeInfo `json:"replay"`
	Score         skilleval.ScoreResult `json:"score"`
	GuardrailPass bool                  `json:"guardrail_pass"`
	Guardrail     string                `json:"guardrail"`
	DryRun        bool                  `json:"dry_run"`
}

func skillevalCommand(args []string) int {
	if len(args) > 0 && args[0] == "list" {
		return skillevalListCommand(args[1:])
	}
	fs := flag.NewFlagSet("skilleval", flag.ContinueOnError)
	bundlePath := fs.String("bundle", "", "path to bundle v2 JSON")
	candidatePath := fs.String("candidate", "", "path to candidate JSON")
	modelRef := fs.String("model", "", "provider/model ref; defaults to default_model")
	jsonOut := fs.Bool("json", false, "print result as JSON")
	dryRun := fs.Bool("dry-run", false, "score without provider replay")
	minBundles := fs.Int("min-bundles", 5, "minimum bundles required by guardrail")
	minScore := fs.Float64("min-score", 0.7, "minimum replay score")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bundlePath == "" || *candidatePath == "" {
		fmt.Fprintln(os.Stderr, "skilleval requires --bundle and --candidate")
		return 2
	}
	bundle, err := skilleval.LoadBundle(*bundlePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	candidate, err := loadSkillEvalCandidate(*candidatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var replayed skilleval.OutcomeInfo
	if *dryRun {
		replayed = skilleval.DryRunReplay(*bundle, candidate)
	} else {
		prov, err := resolveConfiguredSkillEvalProvider(*modelRef)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		replayed, err = (skilleval.ReplayRunner{Provider: prov}).Run(context.Background(), *bundle, candidate)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	score, err := skilleval.ScoreReplay(context.Background(), nil, bundle.Outcome, replayed)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	guard := skilleval.CheckPromotionGuardrail([]skilleval.BundleV2{*bundle}, []skilleval.OutcomeInfo{bundle.Outcome}, []skilleval.OutcomeInfo{replayed}, []skilleval.ScoreResult{score}, candidate, skilleval.GuardrailConfig{MinBundles: *minBundles, MinScore: *minScore})
	summary := skillevalSummary{
		BundleID:      bundle.ID,
		CandidateHash: candidate.Hash,
		Replay:        replayed,
		Score:         score,
		GuardrailPass: guard.Pass,
		Guardrail:     guard.Reason,
		DryRun:        *dryRun,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(summary); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return boolExit(guard.Pass)
	}
	fmt.Printf("bundle: %s\ncandidate: %s\nreplay: %s\nscore: %.2f\nrail: %s\n", summary.BundleID, summary.CandidateHash, summary.Replay.FinalAnswer, summary.Score.Score, summary.Guardrail)
	return boolExit(guard.Pass)
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

func resolveConfiguredSkillEvalProvider(modelRef string) (provider.Provider, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		ref = strings.TrimSpace(cfg.DefaultModel)
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, fmt.Errorf("model %q is not configured", ref)
	}
	return boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
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
