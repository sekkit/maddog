package skilleval

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"maddog/internal/provider"
)

type EvaluationRequest struct {
	Candidate   Candidate
	BundlePaths []string
	Bundles     []BundleV2
	Provider    provider.Provider
	Scorer      provider.Provider
	ModelRef    string
	DryRun      bool
	MinBundles  int
	MinScore    float64
	MaxTokens   int
}

type EvaluationResult struct {
	Bundles        []BundleV2
	BundleIDs      []string
	Baseline       []OutcomeInfo
	Replays        []OutcomeInfo
	Scores         []ScoreResult
	Score          ScoreResult
	Guardrail      GuardrailResult
	DryRun         bool
	Mode           string
	Provider       string
	ModelRef       string
	PromotionGrade bool
}

func (r EvaluationResult) Provenance() EvaluationProvenance {
	return EvaluationProvenance{
		Mode:           r.Mode,
		Provider:       r.Provider,
		ModelRef:       r.ModelRef,
		BundleIDs:      r.BundleIDs,
		PromotionGrade: r.PromotionGrade,
	}
}

func EvaluateCandidate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	bundles, bundleIDs, err := loadEvaluationBundles(req.BundlePaths, req.Bundles)
	if err != nil {
		return EvaluationResult{}, err
	}
	if len(bundles) == 0 {
		return EvaluationResult{}, fmt.Errorf("skill evaluation requires at least one held-out bundle")
	}
	if err := ValidateHeldOutBundles(req.BundlePaths, bundles, req.Candidate); err != nil {
		return EvaluationResult{}, err
	}
	mode := EvaluationModeProviderReplay
	providerName := ""
	if req.DryRun {
		mode = EvaluationModeDryRunPreview
	} else {
		if req.Provider == nil {
			return EvaluationResult{}, fmt.Errorf("promotion-grade skill evaluation requires a provider")
		}
		providerName = req.Provider.Name()
	}
	scorer := req.Scorer
	if scorer == nil && !req.DryRun {
		scorer = req.Provider
	}
	baseline := make([]OutcomeInfo, 0, len(bundles))
	replays := make([]OutcomeInfo, 0, len(bundles))
	scores := make([]ScoreResult, 0, len(bundles))
	for _, bundle := range bundles {
		var replayed OutcomeInfo
		if req.DryRun {
			replayed = DryRunReplay(bundle, req.Candidate)
		} else {
			replayed, err = (ReplayRunner{Provider: req.Provider, MaxTokens: req.MaxTokens}).Run(ctx, bundle, req.Candidate)
			if err != nil {
				return EvaluationResult{}, err
			}
		}
		score, err := ScoreReplayWithContext(ctx, scorer, ScoreReplayRequest{
			Original:          bundle.Outcome,
			Replayed:          replayed,
			Bundle:            bundle,
			Candidate:         req.Candidate,
			RequireModelScore: !req.DryRun,
		})
		if err != nil {
			return EvaluationResult{}, err
		}
		baseline = append(baseline, bundle.Outcome)
		replays = append(replays, replayed)
		scores = append(scores, score)
	}
	guard := CheckPromotionGuardrail(bundles, baseline, replays, scores, req.Candidate, GuardrailConfig{MinBundles: req.MinBundles, MinScore: req.MinScore})
	if req.DryRun && !strings.HasPrefix(guard.Reason, "need at least ") {
		guard = GuardrailResult{Reason: "dry-run preview is not promotion-grade evidence"}
	}
	return EvaluationResult{
		Bundles:        bundles,
		BundleIDs:      bundleIDs,
		Baseline:       baseline,
		Replays:        replays,
		Scores:         scores,
		Score:          AggregateScores(scores),
		Guardrail:      guard,
		DryRun:         req.DryRun,
		Mode:           mode,
		Provider:       providerName,
		ModelRef:       strings.TrimSpace(req.ModelRef),
		PromotionGrade: !req.DryRun && guard.Pass,
	}, nil
}

func loadEvaluationBundles(paths []string, supplied []BundleV2) ([]BundleV2, []string, error) {
	bundles := make([]BundleV2, 0, len(paths)+len(supplied))
	ids := make([]string, 0, len(paths)+len(supplied))
	for _, path := range paths {
		bundle, err := LoadBundle(path)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(bundle.Path) == "" {
			bundle.Path = path
		}
		bundles = append(bundles, *bundle)
		ids = append(ids, bundle.ID)
	}
	for _, bundle := range supplied {
		bundles = append(bundles, bundle)
		ids = append(ids, bundle.ID)
	}
	return bundles, ids, nil
}

func AggregateScores(scores []ScoreResult) ScoreResult {
	if len(scores) == 0 {
		return ScoreResult{}
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
	return ScoreResult{
		Score:  avg,
		Reason: fmt.Sprintf("average score across %d bundles (min %.2f, max %.2f)", len(scores), min, max),
	}
}

func ValidateHeldOutBundles(paths []string, bundles []BundleV2, candidate Candidate) error {
	seenIDs := map[string]bool{}
	seenPaths := map[string]bool{}
	sourceID := strings.TrimSpace(candidate.SourceBundleID)
	sourcePath := CanonicalSkillEvalPath(candidate.SourceBundlePath)
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
		path := CanonicalSkillEvalPath(firstNonEmptyString(inputPath, bundle.Path))
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

func CanonicalSkillEvalPath(path string) string {
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

func cleanStringList(vals []string) []string {
	if len(vals) == 0 {
		return nil
	}
	out := make([]string, 0, len(vals))
	for _, val := range vals {
		val = strings.TrimSpace(val)
		if val != "" {
			out = append(out, val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
