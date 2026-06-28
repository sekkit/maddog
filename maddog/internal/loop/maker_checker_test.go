package loop

import "testing"

func TestMakerCheckerOffAndApprovedCanComplete(t *testing.T) {
	off := EvaluateMakerChecker(MakerCheckerInput{Mode: MakerCheckerOff})
	if !off.CanComplete || off.HumanGate != nil {
		t.Fatalf("off mode should allow completion without gate: %+v", off)
	}
	approved := EvaluateMakerChecker(MakerCheckerInput{
		Mode:    MakerCheckerEnforcedBeforeDone,
		Verdict: CheckerApproved,
	})
	if !approved.CanComplete || approved.HumanGate != nil {
		t.Fatalf("approved enforced checker should allow completion: %+v", approved)
	}
}

func TestMakerCheckerEnforcedAllowsSingleChangesRequestedRetry(t *testing.T) {
	result := EvaluateMakerChecker(MakerCheckerInput{
		Mode:            MakerCheckerEnforcedBeforeDone,
		MakerProvider:   "openai",
		MakerModel:      "gpt-4.1",
		CheckerProvider: "anthropic",
		CheckerModel:    "claude-sonnet-4",
		Verdict:         CheckerChangesRequested,
		RetryCount:      0,
		MaxRetries:      1,
	})
	if result.CanComplete || !result.RetryAllowed || result.Isolation != IsolationStrong {
		t.Fatalf("first changes-requested verdict should retry with strong isolation: %+v", result)
	}

	result = EvaluateMakerChecker(MakerCheckerInput{
		Mode:       MakerCheckerEnforcedBeforeDone,
		Verdict:    CheckerChangesRequested,
		RetryCount: 1,
		MaxRetries: 1,
	})
	if result.RetryAllowed || result.HumanGate == nil || result.HumanGate.Kind != HumanGateCheckerVerdict {
		t.Fatalf("second changes-requested verdict should require human gate: %+v", result)
	}
}

func TestMakerCheckerBlockedAndNeedsHumanRequireGate(t *testing.T) {
	for _, verdict := range []CheckerVerdict{CheckerBlocked, CheckerNeedsHuman} {
		result := EvaluateMakerChecker(MakerCheckerInput{
			Mode:    MakerCheckerEnforcedBeforeDone,
			Verdict: verdict,
		})
		if result.CanComplete || result.HumanGate == nil || result.HumanGate.Kind != HumanGateCheckerVerdict {
			t.Fatalf("verdict %s should require human gate: %+v", verdict, result)
		}
	}
}

func TestMakerCheckerIsolationWeakWhenSameProviderAndModel(t *testing.T) {
	result := EvaluateMakerChecker(MakerCheckerInput{
		Mode:            MakerCheckerReviewOnly,
		MakerProvider:   "openai",
		MakerModel:      "gpt-4.1",
		CheckerProvider: "openai",
		CheckerModel:    "gpt-4.1",
		Verdict:         CheckerApproved,
	})
	if result.Isolation != IsolationWeak || !result.CanComplete {
		t.Fatalf("same provider/model should be weak isolation but review-only can complete: %+v", result)
	}
}

func TestHumanGatePolicyCoversRiskyOperations(t *testing.T) {
	policy := []HumanGateKind{HumanGateGitPush, HumanGateDeleteFiles, HumanGateCredentialChange, HumanGateBudgetIncrease, HumanGateSkillPromotion}
	for _, kind := range policy {
		gate := EvaluateHumanGate(kind, policy, "needs approval")
		if !gate.Required || gate.Kind != kind || gate.Status != "needs_human" {
			t.Fatalf("gate %s = %+v", kind, gate)
		}
	}
	if gate := EvaluateHumanGate(HumanGateGitPush, []HumanGateKind{}, ""); gate.Required {
		t.Fatalf("gate should not be required without policy: %+v", gate)
	}
}

func TestRefinementStrategyIsDefaultOnAndGateControlled(t *testing.T) {
	strategy := TemplateRefinementStrategy{
		Enabled:               true,
		SearchModes:           []RefinementSearchMode{RefinementSearchBFSHypothesis, RefinementSearchDFSCorrection},
		CritiqueRounds:        2,
		CorrectionRounds:      2,
		FinalJudgeIsolation:   IsolationStrong,
		BudgetCapTokens:       500,
		KillSwitchRequired:    true,
		HumanApprovalRequired: true,
	}
	noKill := EvaluateRefinementStrategy(RefinementInput{
		Strategy:              strategy,
		BudgetRemainingTokens: 1000,
		KillSwitchEnabled:     false,
		HumanApproved:         true,
	})
	if noKill.CanStart || noKill.Status != "blocked" || noKill.Reason == "" {
		t.Fatalf("missing kill switch should block refinement: %+v", noKill)
	}

	needsHuman := EvaluateRefinementStrategy(RefinementInput{Strategy: strategy, BudgetRemainingTokens: 1000, KillSwitchEnabled: true, HumanApproved: false})
	if needsHuman.CanStart || needsHuman.HumanGate == nil || needsHuman.HumanGate.Kind != HumanGateBudgetIncrease {
		t.Fatalf("expensive refinement should require human approval: %+v", needsHuman)
	}

	overBudget := EvaluateRefinementStrategy(RefinementInput{Strategy: strategy, BudgetRemainingTokens: 100, KillSwitchEnabled: true, HumanApproved: true})
	if overBudget.CanStart || overBudget.Status != "blocked" || overBudget.Reason == "" {
		t.Fatalf("budget cap should block when remaining tokens are low: %+v", overBudget)
	}

	ready := EvaluateRefinementStrategy(RefinementInput{Strategy: strategy, BudgetRemainingTokens: 1000, KillSwitchEnabled: true, HumanApproved: true})
	if !ready.CanStart || ready.Status != "ready" || ready.FinalJudgeIsolation != IsolationStrong {
		t.Fatalf("approved refinement should be ready with final judge isolation: %+v", ready)
	}
}
