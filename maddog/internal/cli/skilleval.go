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
	BundleID      string                  `json:"bundle_id"`
	BundleIDs     []string                `json:"bundle_ids,omitempty"`
	Bundles       int                     `json:"bundles"`
	CandidateHash string                  `json:"candidate_hash"`
	Replay        skilleval.OutcomeInfo   `json:"replay"`
	Replays       []skilleval.OutcomeInfo `json:"replays,omitempty"`
	Score         skilleval.ScoreResult   `json:"score"`
	Scores        []skilleval.ScoreResult `json:"scores,omitempty"`
	GuardrailPass bool                    `json:"guardrail_pass"`
	Guardrail     string                  `json:"guardrail"`
	DryRun        bool                    `json:"dry_run"`
	Persisted     bool                    `json:"persisted,omitempty"`
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
	bundles := make([]skilleval.BundleV2, 0, len(bundlePaths))
	bundleIDs := make([]string, 0, len(bundlePaths))
	for _, path := range bundlePaths {
		bundle, err := skilleval.LoadBundle(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		bundles = append(bundles, *bundle)
		bundleIDs = append(bundleIDs, bundle.ID)
	}
	if err := validateHeldOutBundles(bundlePaths, bundles, candidate); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var prov provider.Provider
	if !*dryRun {
		prov, err = resolveConfiguredSkillEvalProvider(*modelRef)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	baseline := make([]skilleval.OutcomeInfo, 0, len(bundles))
	replays := make([]skilleval.OutcomeInfo, 0, len(bundles))
	scores := make([]skilleval.ScoreResult, 0, len(bundles))
	for _, bundle := range bundles {
		var replayed skilleval.OutcomeInfo
		if *dryRun {
			replayed = skilleval.DryRunReplay(bundle, candidate)
		} else {
			replayed, err = (skilleval.ReplayRunner{Provider: prov}).Run(context.Background(), bundle, candidate)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		score, err := skilleval.ScoreReplay(context.Background(), scorerProvider(*dryRun, prov), bundle.Outcome, replayed)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		baseline = append(baseline, bundle.Outcome)
		replays = append(replays, replayed)
		scores = append(scores, score)
	}
	guard := skilleval.CheckPromotionGuardrail(bundles, baseline, replays, scores, candidate, skilleval.GuardrailConfig{MinBundles: *minBundles, MinScore: *minScore})
	summaryScore := aggregateSkillEvalScores(scores)
	persisted := false
	if strings.TrimSpace(*storeDir) != "" {
		if _, err := skilleval.NewCandidateStore(*storeDir).RecordEvaluation(candidate.Hash, summaryScore, guard); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		persisted = true
	}
	summary := skillevalSummary{
		BundleID:      bundleIDs[0],
		BundleIDs:     bundleIDs,
		Bundles:       len(bundles),
		CandidateHash: candidate.Hash,
		Replay:        replays[0],
		Replays:       replays,
		Score:         summaryScore,
		Scores:        scores,
		GuardrailPass: guard.Pass,
		Guardrail:     guard.Reason,
		DryRun:        *dryRun,
		Persisted:     persisted,
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
	fmt.Printf("bundles: %d\ncandidate: %s\nreplay: %s\nscore: %.2f\nrail: %s\n", summary.Bundles, summary.CandidateHash, summary.Replay.FinalAnswer, summary.Score.Score, summary.Guardrail)
	return boolExit(guard.Pass)
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

func scorerProvider(dryRun bool, prov provider.Provider) provider.Provider {
	if dryRun {
		return nil
	}
	return prov
}

func aggregateSkillEvalScores(scores []skilleval.ScoreResult) skilleval.ScoreResult {
	if len(scores) == 0 {
		return skilleval.ScoreResult{}
	}
	if len(scores) == 1 {
		return scores[0]
	}
	total := 0.0
	min := scores[0].Score
	max := scores[0].Score
	for _, score := range scores {
		total += score.Score
		if score.Score < min {
			min = score.Score
		}
		if score.Score > max {
			max = score.Score
		}
	}
	avg := total / float64(len(scores))
	return skilleval.ScoreResult{
		Score:  avg,
		Reason: fmt.Sprintf("average score across %d bundles (min %.2f, max %.2f)", len(scores), min, max),
	}
}

func validateHeldOutBundles(paths []string, bundles []skilleval.BundleV2, candidate skilleval.Candidate) error {
	seenIDs := map[string]bool{}
	seenPaths := map[string]bool{}
	sourceID := strings.TrimSpace(candidate.SourceBundleID)
	sourcePath := canonicalSkillEvalPath(candidate.SourceBundlePath)
	for i, bundle := range bundles {
		id := strings.TrimSpace(bundle.ID)
		if id == "" {
			return fmt.Errorf("held-out bundle %d has no id", i+1)
		}
		if seenIDs[id] {
			return fmt.Errorf("duplicate held-out bundle id %q", id)
		}
		seenIDs[id] = true
		if sourceID != "" && id == sourceID {
			return fmt.Errorf("bundle %q is the candidate source bundle, not held-out", id)
		}
		var inputPath string
		if i < len(paths) {
			inputPath = paths[i]
		}
		path := canonicalSkillEvalPath(firstNonEmptyString(inputPath, bundle.Path))
		if path == "" {
			continue
		}
		if seenPaths[path] {
			return fmt.Errorf("duplicate held-out bundle path %q", path)
		}
		seenPaths[path] = true
		if sourcePath != "" && path == sourcePath {
			return fmt.Errorf("bundle %q is the candidate source bundle, not held-out", id)
		}
	}
	return nil
}

func canonicalSkillEvalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return strings.ToLower(filepath.Clean(path))
}

func firstNonEmptyString(vals ...string) string {
	for _, val := range vals {
		if strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
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
