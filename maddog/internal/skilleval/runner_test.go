package skilleval

import (
	"context"
	"testing"
)

func TestReplayRunnerReportsPassRateImprovement(t *testing.T) {
	candidate := Candidate{ID: "cand-1", Validation: ValidationSnapshot{Valid: true}}
	bundles := []Bundle{
		{ID: "bundle-1", Confidence: ConfidenceMedium},
		{ID: "bundle-2", Confidence: ConfidenceMedium},
	}
	report, err := RunReplay(context.Background(), ReplayOptions{
		Candidate:  candidate,
		Bundles:    bundles,
		MinHeldOut: 2,
		Evaluator: ReplayEvaluatorFunc(func(ctx context.Context, candidate Candidate, bundle Bundle) (ReplayCaseReport, error) {
			return ReplayCaseReport{
				ID:               bundle.ID,
				BundleID:         bundle.ID,
				BaselinePassed:   false,
				CandidatePassed:  true,
				BaselineTokens:   100,
				CandidateTokens:  80,
				LowConfidence:    bundle.LowConfidence,
				CandidateSkillID: candidate.ID,
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.HeldOutCases != 2 || report.BaselinePassRate != 0 || report.CandidatePassRate != 1 {
		t.Fatalf("replay pass rates = %+v", report)
	}
	if report.TokenDeltaPercent >= 0 {
		t.Fatalf("candidate should reduce tokens, got delta %.2f", report.TokenDeltaPercent)
	}
}

func TestReplayRunnerMarksInsufficientHeldOutBundles(t *testing.T) {
	report, err := RunReplay(context.Background(), ReplayOptions{
		Candidate:  Candidate{ID: "cand-1", Validation: ValidationSnapshot{Valid: true}},
		Bundles:    []Bundle{{ID: "bundle-1", Confidence: ConfidenceLow, LowConfidence: true}},
		MinHeldOut: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.HeldOutCases != 0 || !report.InsufficientHeldOut {
		t.Fatalf("low-confidence replay bundle should not count as held-out: %+v", report)
	}
}

func TestReplayRunnerCanProduceLongHorizonHarnessProposal(t *testing.T) {
	bundles := []Bundle{{
		ID:         "bundle-1",
		Confidence: ConfidenceMedium,
		CandidateDocs: []CandidateDoc{{
			ID:    "candidate-doc",
			Title: "Candidate doc",
		}},
		CuratedEvidence:     []CuratedEvidence{{ID: "evidence-1", Kind: "failure_signal"}},
		VerificationRecords: []VerificationRecord{{ID: "verify-1", Status: "passed"}},
		Trajectory:          []ActionObservation{{StepID: "step-1", Action: "act", Observation: "observe"}},
		BudgetContext:       ReplayBudgetContext{LimitTokens: 2000, UsedTokens: 400, RemainingTokens: 1600},
	}}

	report, err := RunReplay(context.Background(), ReplayOptions{
		Candidate:              Candidate{ID: "cand-1", Validation: ValidationSnapshot{Valid: true}},
		Bundles:                bundles,
		MinHeldOut:             1,
		IncludeHarnessProposal: true,
		Evaluator: ReplayEvaluatorFunc(func(ctx context.Context, candidate Candidate, bundle Bundle) (ReplayCaseReport, error) {
			return ReplayCaseReport{
				ID:               bundle.ID,
				BundleID:         bundle.ID,
				CandidateSkillID: candidate.ID,
				BaselinePassed:   true,
				CandidatePassed:  false,
				BaselineTokens:   100,
				CandidateTokens:  120,
				Error:            "deterministic failure",
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunReplay: %v", err)
	}
	proposal := report.HarnessProposal
	if proposal == nil {
		t.Fatalf("harness proposal missing: %+v", report)
	}
	if proposal.CandidateDocs != 1 || proposal.CuratedEvidence != 1 || proposal.VerificationRecords != 1 || proposal.TrajectorySteps != 1 {
		t.Fatalf("proposal evidence counts = %+v", proposal)
	}
	if proposal.BudgetLimitTokens != 2000 || proposal.BudgetRemainingTokens != 1600 || proposal.DeterministicFailureCount != 1 {
		t.Fatalf("proposal budget/failure accounting = %+v", proposal)
	}
	for _, excluded := range []string{"training", "cuda", "vllm", "checkpoint", "model_serving"} {
		if !containsString(proposal.ExcludedRuntimeDeps, excluded) {
			t.Fatalf("proposal should exclude %q runtime deps: %+v", excluded, proposal.ExcludedRuntimeDeps)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
