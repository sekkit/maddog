package agent

import (
	"fmt"

	"maddog/internal/evidence"
)

// UpgradeDecision is the result of a routing policy evaluation.
type UpgradeDecision struct {
	ShouldUpgrade  bool
	Reason         string
	TargetModel    string
	TriggerAdvisor bool
}

// UpgradePolicy decides whether a turn should continue on a frontier provider.
type UpgradePolicy interface {
	Evaluate(sig evidence.FailureSignal, turn int, frontierTokens int64) UpgradeDecision
}

// ThresholdUpgradePolicy upgrades after repeated failures, with an optional
// frontier output-token budget. A zero Threshold disables the policy.
type ThresholdUpgradePolicy struct {
	Threshold   int
	BudgetLimit int64
	TargetModel string
}

func (p ThresholdUpgradePolicy) FrontierBudgetLimit() int64 { return p.BudgetLimit }

func (p ThresholdUpgradePolicy) Evaluate(sig evidence.FailureSignal, turn int, frontierTokens int64) UpgradeDecision {
	if p.Threshold <= 0 {
		return UpgradeDecision{}
	}
	if p.BudgetLimit > 0 && frontierTokens >= p.BudgetLimit {
		return UpgradeDecision{}
	}
	target := p.TargetModel
	if target == "" {
		target = "frontier"
	}
	if sig.ConsecutiveErrors >= p.Threshold {
		return UpgradeDecision{
			ShouldUpgrade:  true,
			Reason:         fmt.Sprintf("%d consecutive tool failures", sig.ConsecutiveErrors),
			TargetModel:    target,
			TriggerAdvisor: true,
		}
	}
	if sig.ErrorStreak >= p.Threshold*2 {
		return UpgradeDecision{
			ShouldUpgrade:  true,
			Reason:         fmt.Sprintf("%d tool failures in this turn", sig.ErrorStreak),
			TargetModel:    target,
			TriggerAdvisor: true,
		}
	}
	if sig.ErrorStreak > 0 && sig.HealthScore > 0 && sig.HealthScore < 0.3 {
		return UpgradeDecision{
			ShouldUpgrade:  true,
			Reason:         fmt.Sprintf("tool health %.0f%%", sig.HealthScore*100),
			TargetModel:    target,
			TriggerAdvisor: true,
		}
	}
	return UpgradeDecision{}
}
