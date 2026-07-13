package skilleval

import (
	"fmt"
	"strings"

	"maddog/internal/skill"
)

type GuardrailResult struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason,omitempty"`
}

type GuardrailConfig struct {
	MinBundles int
	MinScore   float64
	// MinDelta is used by paired evaluation. A candidate must improve the
	// incumbent aggregate by at least this amount after all other checks pass.
	MinDelta float64
}

// CheckPairedPromotionGuardrail applies the promotion checks to an incumbent
// and candidate evaluated on the exact same held-out cases. A single absolute
// model score is never sufficient: the candidate must clear the score floor,
// beat the incumbent by MinDelta, and use a verified or explicitly independent
// scorer.
func CheckPairedPromotionGuardrail(
	bundles []BundleV2,
	incumbent, candidate []OutcomeInfo,
	incumbentScores, candidateScores []ScoreResult,
	candidateValue Candidate,
	cfg GuardrailConfig,
	scoring ScoringProvenance,
) GuardrailResult {
	if cfg.MinBundles <= 0 {
		cfg.MinBundles = 5
	}
	if cfg.MinScore <= 0 {
		cfg.MinScore = 0.7
	}
	if cfg.MinDelta <= 0 {
		cfg.MinDelta = 0.01
	}
	if len(bundles) < cfg.MinBundles {
		return GuardrailResult{Reason: fmt.Sprintf("need at least %d bundles, got %d", cfg.MinBundles, len(bundles))}
	}
	if len(incumbent) != len(bundles) || len(candidate) != len(bundles) || len(incumbentScores) != len(bundles) || len(candidateScores) != len(bundles) {
		return GuardrailResult{Reason: "paired evaluation returned incomplete results"}
	}
	if candidateValue.Status == CandidateRejected {
		return GuardrailResult{Reason: "candidate is rejected: " + candidateValue.ValidationReason}
	}
	if candidateValue.Status != CandidatePending && candidateValue.Status != CandidatePromoting && candidateValue.Status != CandidatePromoted {
		return GuardrailResult{Reason: fmt.Sprintf("candidate has invalid status %q", candidateValue.Status)}
	}
	if !candidateValue.Validation.Valid {
		return GuardrailResult{Reason: "candidate validation failed: " + candidateValue.Validation.Reason}
	}
	if verdict := skill.NewValidator().Validate(candidateValue.Skill, candidateValue.SourceTask); !verdict.Valid {
		return GuardrailResult{Reason: "candidate validation failed: " + verdict.Reason}
	}
	if expanded := expandedAllowedTools(bundles, candidateValue); len(expanded) > 0 {
		return GuardrailResult{Reason: "allowed tools expanded: " + strings.Join(expanded, ", ")}
	}
	for i := range bundles {
		if bundles[i].Outcome.Confidence != OutcomeConfidenceVerified {
			return GuardrailResult{Reason: fmt.Sprintf("bundle %s outcome is not verified", bundleLabel(bundles[i], i))}
		}
		if candidateScores[i].Score < cfg.MinScore {
			return GuardrailResult{Reason: fmt.Sprintf("candidate score %.2f below threshold %.2f", candidateScores[i].Score, cfg.MinScore)}
		}
	}
	if reason := pairedScoringReason(scoring); reason != "" {
		return GuardrailResult{Reason: reason}
	}
	old := AggregateScores(incumbentScores)
	new := AggregateScores(candidateScores)
	delta := new.Score - old.Score
	if delta < cfg.MinDelta {
		return GuardrailResult{Reason: fmt.Sprintf("paired score delta %.3f below minimum %.3f", delta, cfg.MinDelta)}
	}
	if oldRate, oldOK := verifiedSuccessRate(incumbent); oldOK {
		if newRate, newOK := verifiedSuccessRate(candidate); newOK && newRate < oldRate {
			return GuardrailResult{Reason: fmt.Sprintf("regression: candidate success %.2f < incumbent %.2f", newRate, oldRate)}
		}
	}
	if costSpike := abnormalCostIncrease(incumbent, candidate); costSpike != "" {
		return GuardrailResult{Reason: costSpike}
	}
	return GuardrailResult{Pass: true, Reason: fmt.Sprintf("paired guardrail passed: score delta %.3f", delta)}
}

func pairedScoringReason(scoring ScoringProvenance) string {
	kind := strings.ToLower(strings.TrimSpace(scoring.Kind))
	if kind == "verified_grader" || kind == "deterministic_grader" || kind == "verifier" {
		if !scoring.Verified || strings.TrimSpace(scoring.Fingerprint) == "" {
			return "paired promotion requires a verified grader fingerprint"
		}
		return ""
	}
	if !scoring.Independent {
		return "paired promotion requires an explicitly independent scorer"
	}
	if strings.TrimSpace(scoring.Provider) == "" || strings.TrimSpace(scoring.ReplayProvider) == "" || strings.EqualFold(strings.TrimSpace(scoring.Provider), strings.TrimSpace(scoring.ReplayProvider)) {
		return "independent scorer must use a provider different from replay provider"
	}
	return ""
}

func CheckPromotionGuardrail(bundles []BundleV2, baseline, replayed []OutcomeInfo, scores []ScoreResult, candidate Candidate, cfg GuardrailConfig) GuardrailResult {
	if cfg.MinBundles <= 0 {
		cfg.MinBundles = 5
	}
	if cfg.MinScore <= 0 {
		cfg.MinScore = 0.7
	}
	if candidate.Status == CandidateRejected {
		return GuardrailResult{Reason: "candidate is rejected: " + candidate.ValidationReason}
	}
	if candidate.Status != CandidatePending && candidate.Status != CandidatePromoting && candidate.Status != CandidatePromoted {
		return GuardrailResult{Reason: fmt.Sprintf("candidate has invalid status %q", candidate.Status)}
	}
	if !candidate.Validation.Valid {
		return GuardrailResult{Reason: "candidate validation failed: " + candidate.Validation.Reason}
	}
	if verdict := skill.NewValidator().Validate(candidate.Skill, candidate.SourceTask); !verdict.Valid {
		return GuardrailResult{Reason: "candidate validation failed: " + verdict.Reason}
	}
	if len(bundles) < cfg.MinBundles {
		return GuardrailResult{Reason: fmt.Sprintf("need at least %d bundles, got %d", cfg.MinBundles, len(bundles))}
	}
	if len(baseline) < len(bundles) || len(replayed) < len(bundles) || len(scores) < len(bundles) {
		return GuardrailResult{Reason: "missing replay or score results"}
	}
	if reason := unverifiedOutcomeReason(bundles, baseline, replayed); reason != "" {
		return GuardrailResult{Reason: reason}
	}
	if expanded := expandedAllowedTools(bundles, candidate); len(expanded) > 0 {
		return GuardrailResult{Reason: "allowed tools expanded: " + strings.Join(expanded, ", ")}
	}
	oldRate, oldComparable := verifiedSuccessRate(baseline[:len(bundles)])
	newRate, newComparable := verifiedSuccessRate(replayed[:len(bundles)])
	if oldComparable && newComparable && newRate < oldRate {
		return GuardrailResult{Reason: fmt.Sprintf("regression: new success %.2f < old %.2f", newRate, oldRate)}
	}
	if costSpike := abnormalCostIncrease(baseline[:len(bundles)], replayed[:len(bundles)]); costSpike != "" {
		return GuardrailResult{Reason: costSpike}
	}
	for i := 0; i < len(bundles); i++ {
		if scores[i].Score < cfg.MinScore {
			return GuardrailResult{Reason: fmt.Sprintf("score %.2f below threshold %.2f", scores[i].Score, cfg.MinScore)}
		}
	}
	if oldComparable && newComparable {
		return GuardrailResult{Pass: true, Reason: fmt.Sprintf("passed guardrail: success %.2f >= %.2f", newRate, oldRate)}
	}
	return GuardrailResult{Pass: true, Reason: "passed guardrail: unverified replay quality accepted by promotion-grade scores"}
}

func unverifiedOutcomeReason(bundles []BundleV2, baseline, replayed []OutcomeInfo) string {
	for i := 0; i < len(bundles); i++ {
		if bundles[i].Outcome.Confidence != OutcomeConfidenceVerified {
			return fmt.Sprintf("bundle %s outcome is not verified", bundleLabel(bundles[i], i))
		}
		if baseline[i].Confidence != OutcomeConfidenceVerified {
			return fmt.Sprintf("baseline outcome for bundle %s is not verified", bundleLabel(bundles[i], i))
		}
	}
	return ""
}

func bundleLabel(bundle BundleV2, index int) string {
	if strings.TrimSpace(bundle.ID) != "" {
		return bundle.ID
	}
	return fmt.Sprintf("#%d", index+1)
}

func abnormalCostIncrease(baseline, replayed []OutcomeInfo) string {
	var oldTokens, newTokens int
	for i := range baseline {
		oldTokens += baseline[i].Tokens
		newTokens += replayed[i].Tokens
	}
	if oldTokens > 0 && newTokens > oldTokens*2 {
		return fmt.Sprintf("cost increase: replay tokens %d > 2x baseline %d", newTokens, oldTokens)
	}
	return ""
}

func successRate(results []OutcomeInfo) float64 {
	if len(results) == 0 {
		return 0
	}
	ok := 0
	for _, r := range results {
		if r.Success && r.GoalMet {
			ok++
		}
	}
	return float64(ok) / float64(len(results))
}

func verifiedSuccessRate(results []OutcomeInfo) (float64, bool) {
	if len(results) == 0 {
		return 0, false
	}
	for _, r := range results {
		if r.Confidence != OutcomeConfidenceVerified {
			return 0, false
		}
	}
	return successRate(results), true
}

func expandedAllowedTools(bundles []BundleV2, candidate Candidate) []string {
	baseline := map[string]bool{}
	for _, b := range bundles {
		collectSkillTools(b.Dynamic, candidate.Skill.Name, baseline)
		for i := range b.Skills {
			collectSkillTools(&b.Skills[i], candidate.Skill.Name, baseline)
		}
	}
	var expanded []string
	for _, tool := range candidate.Skill.AllowedTools {
		tool = strings.TrimSpace(tool)
		if tool == "" || baseline[tool] || !isHighRiskTool(tool) {
			continue
		}
		expanded = append(expanded, tool)
	}
	return expanded
}

func isHighRiskTool(tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	return strings.Contains(tool, "write") ||
		strings.Contains(tool, "edit") ||
		strings.Contains(tool, "delete") ||
		strings.Contains(tool, "move") ||
		strings.Contains(tool, "memory") ||
		tool == "remember" ||
		tool == "forget"
}

func collectSkillTools(sk *SkillSnapshot, name string, out map[string]bool) {
	if sk == nil || strings.TrimSpace(sk.Name) != strings.TrimSpace(name) {
		return
	}
	for _, tool := range sk.AllowedTools {
		tool = strings.TrimSpace(tool)
		if tool != "" {
			out[tool] = true
		}
	}
}
