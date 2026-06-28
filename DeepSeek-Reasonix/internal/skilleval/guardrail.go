package skilleval

import (
	"fmt"
	"sort"
	"strings"
)

type GuardrailOptions struct {
	ActiveAllowedTools []string
	Replay             ReplayReport
	MinHeldOut         int
}

func EvaluateGuardrails(candidate Candidate, opts GuardrailOptions) ScoreResult {
	if !candidate.Validation.Valid {
		reason := strings.TrimSpace(candidate.Validation.Reason)
		if reason == "" {
			reason = "candidate validator rejected the generated skill"
		}
		return ScoreResult{CandidateID: candidate.ID, Decision: DecisionRejected, Score: 0, Reason: reason}
	}
	if expanded := toolExpansion(candidate.Skill.AllowedTools, opts.ActiveAllowedTools); len(expanded) > 0 {
		return ScoreResult{
			CandidateID: candidate.ID,
			Decision:    DecisionRejected,
			Score:       0,
			Reason:      "allowed-tools expansion rejected: " + strings.Join(expanded, ", "),
		}
	}
	minHeldOut := opts.MinHeldOut
	if minHeldOut <= 0 {
		minHeldOut = opts.Replay.MinHeldOut
	}
	if minHeldOut > 0 && opts.Replay.HeldOutCases < minHeldOut || opts.Replay.InsufficientHeldOut {
		return ScoreResult{
			CandidateID: candidate.ID,
			Decision:    DecisionReviewNeeded,
			Score:       0.2,
			Reason:      fmt.Sprintf("insufficient held-out bundles: %d/%d", opts.Replay.HeldOutCases, minHeldOut),
		}
	}
	if opts.Replay.CandidatePassRate < opts.Replay.BaselinePassRate {
		return ScoreResult{
			CandidateID: candidate.ID,
			Decision:    DecisionRejected,
			Score:       0.1,
			Reason:      "candidate replay pass rate regressed",
		}
	}
	if opts.Replay.CandidatePassRate > opts.Replay.BaselinePassRate {
		return ScoreResult{
			CandidateID: candidate.ID,
			Decision:    DecisionPromotable,
			Score:       0.7,
			Reason:      "guardrails passed with replay improvement",
		}
	}
	return ScoreResult{
		CandidateID: candidate.ID,
		Decision:    DecisionReviewNeeded,
		Score:       0.5,
		Reason:      "guardrails passed; reviewer approval required",
	}
}

func toolExpansion(candidateTools, activeTools []string) []string {
	if len(candidateTools) == 0 || len(activeTools) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	for _, tool := range activeTools {
		if key := strings.ToLower(strings.TrimSpace(tool)); key != "" {
			allowed[key] = true
		}
	}
	var expanded []string
	for _, tool := range candidateTools {
		key := strings.ToLower(strings.TrimSpace(tool))
		if key != "" && !allowed[key] {
			expanded = append(expanded, key)
		}
	}
	sort.Strings(expanded)
	return expanded
}
