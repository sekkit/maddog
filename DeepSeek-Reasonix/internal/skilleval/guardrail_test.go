package skilleval

import (
	"strings"
	"testing"
)

func TestGuardrailRejectsAllowedToolsExpansion(t *testing.T) {
	result := EvaluateGuardrails(Candidate{
		ID: "cand-1",
		Skill: SkillSnapshot{
			Name:         "dynamic-docs",
			AllowedTools: []string{"read_file", "bash"},
		},
		Validation: ValidationSnapshot{Valid: true},
	}, GuardrailOptions{
		ActiveAllowedTools: []string{"read_file"},
		Replay: ReplayReport{
			HeldOutCases:      2,
			CandidatePassRate: 1,
			BaselinePassRate:  0,
		},
		MinHeldOut: 2,
	})

	if result.Decision != DecisionRejected || !strings.Contains(result.Reason, "allowed-tools expansion") {
		t.Fatalf("guardrail result = %+v", result)
	}
}

func TestGuardrailAllowsPassingCandidate(t *testing.T) {
	result := EvaluateGuardrails(Candidate{
		ID:         "cand-1",
		Skill:      SkillSnapshot{Name: "dynamic-docs", AllowedTools: []string{"read_file"}},
		Validation: ValidationSnapshot{Valid: true},
	}, GuardrailOptions{
		ActiveAllowedTools: []string{"read_file", "grep"},
		Replay: ReplayReport{
			HeldOutCases:      2,
			CandidatePassRate: 1,
			BaselinePassRate:  0,
		},
		MinHeldOut: 2,
	})
	if result.Decision != DecisionPromotable {
		t.Fatalf("guardrail should allow candidate: %+v", result)
	}
}
