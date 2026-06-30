package skilleval

import (
	"strings"
	"testing"
)

func TestGuardrailPassesAndRejectsPromotionRisks(t *testing.T) {
	bundles := []BundleV2{
		{ID: "a", Dynamic: &SkillSnapshot{Name: "parser-helper", AllowedTools: []string{"read_file"}}},
		{ID: "b", Dynamic: &SkillSnapshot{Name: "parser-helper", AllowedTools: []string{"read_file"}}},
	}
	baseline := []OutcomeInfo{{Success: true, GoalMet: true}, {Success: false}}
	replayed := []OutcomeInfo{{Success: true, GoalMet: true}, {Success: true, GoalMet: true}}
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
	readOnly := CheckPromotionGuardrail([]BundleV2{{ID: "a"}, {ID: "b"}}, baseline, replayed, scores, readOnlyNewCandidate, GuardrailConfig{MinBundles: 2, MinScore: 0.7})
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
