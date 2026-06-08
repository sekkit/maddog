package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

func TestThresholdUpgradePolicy(t *testing.T) {
	policy := ThresholdUpgradePolicy{Threshold: 3, BudgetLimit: 10, TargetModel: "claude"}

	if got := policy.Evaluate(evidence.FailureSignal{ConsecutiveErrors: 2}, 1, 0); got.ShouldUpgrade {
		t.Fatalf("two consecutive errors should not upgrade: %+v", got)
	}
	got := policy.Evaluate(evidence.FailureSignal{ConsecutiveErrors: 3, LastErrorTool: "bash"}, 1, 0)
	if !got.ShouldUpgrade || got.TargetModel != "claude" || !strings.Contains(got.Reason, "3 consecutive") {
		t.Fatalf("three consecutive errors should upgrade to claude, got %+v", got)
	}
	if got := policy.Evaluate(evidence.FailureSignal{ConsecutiveErrors: 3}, 1, 10); got.ShouldUpgrade {
		t.Fatalf("budget-exhausted policy should not upgrade: %+v", got)
	}
	if got := policy.Evaluate(evidence.FailureSignal{ErrorStreak: 6}, 1, 0); !got.ShouldUpgrade {
		t.Fatalf("error streak should upgrade")
	}
	if got := policy.Evaluate(evidence.FailureSignal{ErrorStreak: 1, HealthScore: 0.2}, 1, 0); !got.ShouldUpgrade {
		t.Fatalf("low health with failures should upgrade")
	}
}

func TestRunUpgradesToFrontierAfterRepeatedToolFailures(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c3", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("frontier", testutil.Turn{Text: "frontier fixed it"})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:         ThresholdUpgradePolicy{Threshold: 3, TargetModel: "frontier-model"},
		FrontierProvider:      frontierProv,
		FrontierContextWindow: 1234,
		FrontierTarget:        "frontier-model",
	}, sink)

	if err := a.Run(context.Background(), "make progress"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if defaultProv.CallCount() != 3 {
		t.Fatalf("default provider calls = %d, want 3", defaultProv.CallCount())
	}
	if frontierProv.CallCount() != 1 {
		t.Fatalf("frontier provider calls = %d, want 1", frontierProv.CallCount())
	}
	upgrades := sink.kinds(event.Upgrade)
	if len(upgrades) != 1 || !strings.Contains(upgrades[0].Text, "frontier-model") {
		t.Fatalf("upgrade events = %+v, want one frontier-model event", upgrades)
	}
	last := a.Session().Messages[len(a.Session().Messages)-1]
	if last.Role != provider.RoleAssistant || last.Content != "frontier fixed it" {
		t.Fatalf("final assistant message = %+v", last)
	}
}

func TestRunFallsBackWhenFrontierStreamFails(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{Text: "default fallback answer"},
	)
	frontierProv := testutil.NewMock("frontier", testutil.ErrorTurn(context.Canceled))
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, TargetModel: "frontier"},
		FrontierProvider: frontierProv,
	}, sink)

	if err := a.Run(context.Background(), "try risky route"); err != nil {
		t.Fatalf("Run should fall back after frontier stream failure, got %v", err)
	}
	if frontierProv.CallCount() != 1 {
		t.Fatalf("frontier calls = %d, want 1", frontierProv.CallCount())
	}
	if defaultProv.CallCount() != 3 {
		t.Fatalf("default calls = %d, want 3 including fallback retry", defaultProv.CallCount())
	}
	events := sink.kinds(event.Upgrade)
	if len(events) != 2 || !strings.Contains(events[1].Text, "switched back to default") {
		t.Fatalf("upgrade/fallback events = %+v, want fallback notice", events)
	}
}

func TestRunEmitsBudgetExceededAfterFrontierUsage(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("frontier", testutil.Turn{
		Text:  "frontier answer",
		Usage: &provider.Usage{CompletionTokens: 7, TotalTokens: 7},
	})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, BudgetLimit: 5, TargetModel: "frontier"},
		FrontierProvider: frontierProv,
	}, sink)

	if err := a.Run(context.Background(), "try route with small budget"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := sink.kinds(event.BudgetExceeded)
	if len(events) != 1 || !strings.Contains(events[0].Text, "7/5") {
		t.Fatalf("budget events = %+v, want one 7/5 event", events)
	}
}
