package skilleval

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"maddog/internal/provider"
	"maddog/internal/tool"
)

type EvaluationRequest struct {
	Candidate        Candidate
	BundlePaths      []string
	Bundles          []BundleV2
	Provider         provider.Provider
	Tools            *tool.Registry
	Scorer           provider.Provider
	ScorerProvenance ScoringProvenance
	Grader           VerifiedGrader
	ModelRef         string
	DryRun           bool
	MinBundles       int
	MinScore         float64
	MaxTokens        int
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
	Scoring        ScoringProvenance
	PromotionGrade bool
}

// ScoringProvenance describes the evidence source used to score replay
// outcomes. Promotion requires either a verified grader fingerprint or an
// explicitly independent scorer whose provider differs from the replay model.
type ScoringProvenance struct {
	Kind           string `json:"kind,omitempty"`
	Name           string `json:"name,omitempty"`
	Provider       string `json:"provider,omitempty"`
	ModelRef       string `json:"model_ref,omitempty"`
	Fingerprint    string `json:"fingerprint,omitempty"`
	ReplayProvider string `json:"replay_provider,omitempty"`
	Independent    bool   `json:"independent,omitempty"`
	Verified       bool   `json:"verified,omitempty"`
}

type VerifiedGradeRequest struct {
	Bundle    BundleV2
	Candidate Candidate
	Replay    OutcomeInfo
}

type VerifiedGrade struct {
	Score    float64 `json:"score"`
	Success  bool    `json:"success"`
	GoalMet  bool    `json:"goal_met"`
	Verified bool    `json:"verified"`
	Reason   string  `json:"reason,omitempty"`
}

// VerifiedGrader is implemented by deterministic or externally verified case
// graders. Returning Verified=false keeps the case diagnostic-only.
type VerifiedGrader interface {
	Grade(context.Context, VerifiedGradeRequest) (VerifiedGrade, error)
	Provenance() ScoringProvenance
}

type PairedEvaluationRequest struct {
	Incumbent        Candidate
	Candidate        Candidate
	BundlePaths      []string
	Bundles          []BundleV2
	Provider         provider.Provider
	Tools            *tool.Registry
	Scorer           provider.Provider
	ScorerProvenance ScoringProvenance
	Grader           VerifiedGrader
	ModelRef         string
	DryRun           bool
	MinBundles       int
	MinScore         float64
	MinDelta         float64
	MaxTokens        int
}

type PairedCaseResult struct {
	Identity        string      `json:"identity"`
	BundleID        string      `json:"bundle_id"`
	Dataset         string      `json:"dataset,omitempty"`
	Split           string      `json:"split,omitempty"`
	CaseID          string      `json:"case_id,omitempty"`
	IncumbentReplay OutcomeInfo `json:"incumbent_replay"`
	CandidateReplay OutcomeInfo `json:"candidate_replay"`
	IncumbentScore  ScoreResult `json:"incumbent_score"`
	CandidateScore  ScoreResult `json:"candidate_score"`
	Delta           float64     `json:"delta"`
}

type PairedEvaluationResult struct {
	Bundles        []BundleV2         `json:"bundles"`
	BundleIDs      []string           `json:"bundle_ids"`
	Cases          []PairedCaseResult `json:"cases"`
	Incumbent      EvaluationResult   `json:"incumbent"`
	Candidate      EvaluationResult   `json:"candidate"`
	Delta          float64            `json:"delta"`
	Guardrail      GuardrailResult    `json:"guardrail"`
	Scoring        ScoringProvenance  `json:"scoring"`
	PromotionGrade bool               `json:"promotion_grade"`
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
	result, err := evaluateCandidateOnBundles(ctx, req, bundles, bundleIDs)
	if err != nil {
		return EvaluationResult{}, err
	}
	if req.DryRun && !strings.HasPrefix(result.Guardrail.Reason, "need at least ") {
		result.Guardrail = GuardrailResult{Reason: "dry-run preview is not promotion-grade evidence"}
	} else if result.Guardrail.Pass {
		result.Guardrail = GuardrailResult{Reason: "promotion requires paired incumbent/candidate evaluation"}
	}
	result.PromotionGrade = false
	return result, nil
}

func EvaluatePairedCandidates(ctx context.Context, req PairedEvaluationRequest) (PairedEvaluationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	bundles, bundleIDs, err := loadEvaluationBundles(req.BundlePaths, req.Bundles)
	if err != nil {
		return PairedEvaluationResult{}, err
	}
	if len(bundles) == 0 {
		return PairedEvaluationResult{}, fmt.Errorf("paired skill evaluation requires at least one held-out bundle")
	}
	if err := ValidateHeldOutBundles(req.BundlePaths, bundles, req.Incumbent); err != nil {
		return PairedEvaluationResult{}, fmt.Errorf("incumbent held-out validation: %w", err)
	}
	if err := ValidateHeldOutBundles(req.BundlePaths, bundles, req.Candidate); err != nil {
		return PairedEvaluationResult{}, fmt.Errorf("candidate held-out validation: %w", err)
	}
	common := EvaluationRequest{
		BundlePaths:      req.BundlePaths,
		Bundles:          bundles,
		Provider:         req.Provider,
		Tools:            req.Tools,
		Scorer:           req.Scorer,
		ScorerProvenance: req.ScorerProvenance,
		Grader:           req.Grader,
		ModelRef:         req.ModelRef,
		DryRun:           req.DryRun,
		MinBundles:       req.MinBundles,
		MinScore:         req.MinScore,
		MaxTokens:        req.MaxTokens,
	}
	incumbentReq := common
	incumbentReq.Candidate = req.Incumbent
	incumbent, err := evaluateCandidateOnBundles(ctx, incumbentReq, bundles, bundleIDs)
	if err != nil {
		return PairedEvaluationResult{}, fmt.Errorf("evaluate incumbent: %w", err)
	}
	candidateReq := common
	candidateReq.Candidate = req.Candidate
	candidate, err := evaluateCandidateOnBundles(ctx, candidateReq, bundles, bundleIDs)
	if err != nil {
		return PairedEvaluationResult{}, fmt.Errorf("evaluate candidate: %w", err)
	}
	if len(incumbent.Replays) != len(bundles) || len(candidate.Replays) != len(bundles) || len(incumbent.Scores) != len(bundles) || len(candidate.Scores) != len(bundles) {
		return PairedEvaluationResult{}, fmt.Errorf("paired evaluation returned incomplete case results")
	}
	cases := make([]PairedCaseResult, 0, len(bundles))
	for i := range bundles {
		cases = append(cases, PairedCaseResult{
			Identity:        EvaluationCaseIdentity(bundles[i]),
			BundleID:        bundles[i].ID,
			Dataset:         bundles[i].Dataset,
			Split:           bundles[i].Split,
			CaseID:          bundles[i].CaseID,
			IncumbentReplay: incumbent.Replays[i],
			CandidateReplay: candidate.Replays[i],
			IncumbentScore:  incumbent.Scores[i],
			CandidateScore:  candidate.Scores[i],
			Delta:           candidate.Scores[i].Score - incumbent.Scores[i].Score,
		})
	}
	guard := CheckPairedPromotionGuardrail(
		bundles,
		incumbent.Replays,
		candidate.Replays,
		incumbent.Scores,
		candidate.Scores,
		req.Candidate,
		GuardrailConfig{MinBundles: req.MinBundles, MinScore: req.MinScore, MinDelta: req.MinDelta},
		candidate.Scoring,
	)
	if req.DryRun && !strings.HasPrefix(guard.Reason, "need at least ") {
		guard = GuardrailResult{Reason: "dry-run preview is not promotion-grade evidence"}
	}
	delta := candidate.Score.Score - incumbent.Score.Score
	candidate.Guardrail = guard
	candidate.PromotionGrade = !req.DryRun && guard.Pass
	return PairedEvaluationResult{
		Bundles:        bundles,
		BundleIDs:      bundleIDs,
		Cases:          cases,
		Incumbent:      incumbent,
		Candidate:      candidate,
		Delta:          delta,
		Guardrail:      guard,
		Scoring:        candidate.Scoring,
		PromotionGrade: candidate.PromotionGrade,
	}, nil
}

func evaluateCandidateOnBundles(ctx context.Context, req EvaluationRequest, bundles []BundleV2, bundleIDs []string) (EvaluationResult, error) {
	mode := EvaluationModeProviderReplay
	providerName := ""
	if req.DryRun {
		mode = EvaluationModeDryRunPreview
	} else {
		if req.Provider == nil {
			return EvaluationResult{}, fmt.Errorf("promotion-grade skill evaluation requires a provider")
		}
		providerName = req.Provider.Name()
		if req.Tools != nil && req.Tools.Len() > 0 {
			mode = EvaluationModeAgentReplay
		}
	}
	scorer := req.Scorer
	if scorer == nil && !req.DryRun {
		scorer = req.Provider
	}
	scoring := evaluationScoringProvenance(req, providerName, scorer)
	baseline := make([]OutcomeInfo, 0, len(bundles))
	replays := make([]OutcomeInfo, 0, len(bundles))
	scores := make([]ScoreResult, 0, len(bundles))
	for _, bundle := range bundles {
		var replayed OutcomeInfo
		var err error
		if req.DryRun {
			replayed = DryRunReplay(bundle, req.Candidate)
		} else if req.Tools != nil && req.Tools.Len() > 0 {
			replayed, err = (AgentReplayRunner{Provider: req.Provider, Tools: req.Tools, MaxSteps: 8}).Run(ctx, bundle, req.Candidate)
			if err != nil {
				return EvaluationResult{}, err
			}
		} else {
			replayed, err = (ReplayRunner{Provider: req.Provider, MaxTokens: req.MaxTokens}).Run(ctx, bundle, req.Candidate)
			if err != nil {
				return EvaluationResult{}, err
			}
		}
		var score ScoreResult
		if req.Grader != nil && !req.DryRun {
			grade, gradeErr := req.Grader.Grade(ctx, VerifiedGradeRequest{Bundle: bundle, Candidate: req.Candidate, Replay: replayed})
			if gradeErr != nil {
				return EvaluationResult{}, gradeErr
			}
			score = scoreFromVerifiedGrade(grade)
			replayed = applyVerifiedGrade(replayed, grade, scoring)
		} else {
			score, err = ScoreReplayWithContext(ctx, scorer, ScoreReplayRequest{
				Original:          bundle.Outcome,
				Replayed:          replayed,
				Bundle:            bundle,
				Candidate:         req.Candidate,
				RequireModelScore: !req.DryRun,
			})
			if err != nil {
				return EvaluationResult{}, err
			}
		}
		baseline = append(baseline, bundle.Outcome)
		replays = append(replays, replayed)
		scores = append(scores, score)
	}
	guard := CheckPromotionGuardrail(bundles, baseline, replays, scores, req.Candidate, GuardrailConfig{MinBundles: req.MinBundles, MinScore: req.MinScore})
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
		Scoring:        scoring,
		PromotionGrade: false,
	}, nil
}

func evaluationScoringProvenance(req EvaluationRequest, replayProvider string, scorer provider.Provider) ScoringProvenance {
	var out ScoringProvenance
	if req.Grader != nil {
		out = req.Grader.Provenance()
		if strings.TrimSpace(out.Kind) == "" {
			out.Kind = "verified_grader"
		}
	} else {
		out = req.ScorerProvenance
		if strings.TrimSpace(out.Kind) == "" && scorer != nil {
			out.Kind = "model_scorer"
		}
		if strings.TrimSpace(out.Provider) == "" && scorer != nil {
			out.Provider = strings.TrimSpace(scorer.Name())
		}
	}
	out.Kind = strings.TrimSpace(out.Kind)
	out.Name = strings.TrimSpace(out.Name)
	out.Provider = strings.TrimSpace(out.Provider)
	out.ModelRef = strings.TrimSpace(out.ModelRef)
	out.Fingerprint = strings.TrimSpace(out.Fingerprint)
	out.ReplayProvider = strings.TrimSpace(replayProvider)
	return out
}

func scoreFromVerifiedGrade(grade VerifiedGrade) ScoreResult {
	score := grade.Score
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	reason := strings.TrimSpace(grade.Reason)
	if reason == "" {
		reason = "verified grader result"
	}
	if !grade.Verified {
		reason = "unverified grader result: " + reason
	}
	return ScoreResult{Score: score, Reason: reason}
}

func applyVerifiedGrade(replayed OutcomeInfo, grade VerifiedGrade, provenance ScoringProvenance) OutcomeInfo {
	replayed.Success = grade.Success
	replayed.GoalMet = grade.GoalMet
	if !grade.Verified {
		replayed.Confidence = OutcomeConfidenceUnverified
		replayed.ConfidenceReason = "grader did not verify this case"
		return replayed
	}
	replayed.Confidence = OutcomeConfidenceVerified
	reason := strings.TrimSpace(grade.Reason)
	if reason == "" {
		reason = strings.TrimSpace(provenance.Name)
	}
	if reason == "" {
		reason = strings.TrimSpace(provenance.Fingerprint)
	}
	if reason == "" {
		reason = "external verifier"
	}
	replayed.ConfidenceReason = "verified by grader: " + reason
	return replayed
}

func EvaluationCaseIdentity(bundle BundleV2) string {
	caseID := strings.TrimSpace(bundle.CaseID)
	if caseID == "" {
		return strings.TrimSpace(bundle.ID)
	}
	parts := make([]string, 0, 3)
	if dataset := strings.TrimSpace(bundle.Dataset); dataset != "" {
		parts = append(parts, dataset)
	}
	if split := strings.TrimSpace(bundle.Split); split != "" {
		parts = append(parts, split)
	}
	parts = append(parts, caseID)
	return strings.Join(parts, "/")
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
