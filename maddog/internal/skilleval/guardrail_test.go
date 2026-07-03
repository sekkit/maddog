package skilleval

import (
	"strings"
	"testing"
)

func TestGuardrailPassesAndRejectsPromotionRisks(t *testing.T) {
	bundles := []BundleV2{
		{ID: "a", Outcome: verifiedOutcome(true), Dynamic: &SkillSnapshot{Name: "parser-helper", AllowedTools: []string{"read_file"}}},
		{ID: "b", Outcome: verifiedOutcome(true), Dynamic: &SkillSnapshot{Name: "parser-helper", AllowedTools: []string{"read_file"}}},
	}
	baseline := []OutcomeInfo{verifiedOutcome(true), verifiedOutcome(false)}
	replayed := []OutcomeInfo{verifiedOutcome(true), verifiedOutcome(true)}
	scores := []ScoreResult{{Score: 0.8}, {Score: 0.9}}
	candidate := Candidate{Hash: "abc", Skill: validScoredSkill("parser-helper", "read_file"), Status: CandidatePending, Validation: ValidationInfo{Valid: true}}

	pass := CheckPromotionGuardrail(bundles, baseline, replayed, scores, candidate, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
	if !pass.Pass {
		t.Fatalf("guardrail should pass: %+v", pass)
	}

	regression := CheckPromotionGuardrail(bundles, replayed, baseline, scores, candidate, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
	if regression.Pass || !strings.Contains(regression.Reason, "regression") {
		t.Fatalf("guardrail should reject regression: %+v", regression)
	}

	lowScore := CheckPromotionGuardrail(bundles, baseline, replayed, []ScoreResult{{Score: 0.8}, {Score: 0.2}}, candidate, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
	if lowScore.Pass || !strings.Contains(lowScore.Reason, "below") {
		t.Fatalf("guardrail should reject low score: %+v", lowScore)
	}

	expanded := candidate
	expanded.Skill.AllowedTools = []string{"read_file", "delete_file"}
	toolRisk := CheckPromotionGuardrail(bundles, baseline, replayed, scores, expanded, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
	if toolRisk.Pass || !strings.Contains(toolRisk.Reason, "allowed tools expanded") {
		t.Fatalf("guardrail should reject tool expansion: %+v", toolRisk)
	}

	readOnlyNewCandidate := candidate
	readOnlyNewCandidate.Skill.AllowedTools = []string{"read_file", "grep"}
	readOnly := CheckPromotionGuardrail([]BundleV2{{ID: "a", Outcome: verifiedOutcome(true)}, {ID: "b", Outcome: verifiedOutcome(true)}}, baseline, replayed, scores, readOnlyNewCandidate, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
	if !readOnly.Pass {
		t.Fatalf("read-only tool expansion should pass: %+v", readOnly)
	}

	rejected := candidate
	rejected.Status = CandidateRejected
	rejected.Validation = ValidationInfo{Valid: false, Reason: "bad"}
	invalid := CheckPromotionGuardrail(bundles, baseline, replayed, scores, rejected, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
	if invalid.Pass || !strings.Contains(invalid.Reason, "rejected") {
		t.Fatalf("guardrail should reject invalid candidate: %+v", invalid)
	}
}

func TestGuardrailRejectsUnverifiedBundleOutcome(t *testing.T) {
	bundles := []BundleV2{{ID: "source", Outcome: OutcomeInfo{Success: true, GoalMet: true}}}
	baseline := []OutcomeInfo{verifiedOutcome(true)}
	replayed := []OutcomeInfo{verifiedOutcome(true)}
	scores := []ScoreResult{{Score: 0.9}}
	candidate := Candidate{Hash: "abc", Skill: validScoredSkill("parser-helper", "read_file"), Status: CandidatePending, Validation: ValidationInfo{Valid: true}}

	result := CheckPromotionGuardrail(bundles, baseline, replayed, scores, candidate, GuardrailConfig{MinBundles: 1, MinScore: 0.7})
	if result.Pass || !strings.Contains(result.Reason, "not verified") {
		t.Fatalf("guardrail should reject unverified bundle outcome: %+v", result)
	}
}

func TestGuardrailAllowsUnverifiedReplayWhenModelScorePasses(t *testing.T) {
	bundles := []BundleV2{{ID: "source", Outcome: verifiedOutcome(true)}}
	baseline := []OutcomeInfo{verifiedOutcome(true)}
	replayed := []OutcomeInfo{{
		Success:          true,
		GoalMet:          true,
		Confidence:       OutcomeConfidenceUnverified,
		ConfidenceReason: "provider replay completion is scored separately",
	}}
	scores := []ScoreResult{{Score: 0.92, Reason: "model scorer accepted replay"}}
	candidate := Candidate{Hash: "abc", Skill: validScoredSkill("parser-helper", "read_file"), Status: CandidatePending, Validation: ValidationInfo{Valid: true}}

	result := CheckPromotionGuardrail(bundles, baseline, replayed, scores, candidate, GuardrailConfig{MinBundles: 1, MinScore: 0.7})
	if !result.Pass {
		t.Fatalf("guardrail should use model score for replay quality, got %+v", result)
	}
}

func verifiedOutcome(success bool) OutcomeInfo {
	return OutcomeInfo{Success: success, GoalMet: success, Confidence: OutcomeConfidenceVerified}
}
