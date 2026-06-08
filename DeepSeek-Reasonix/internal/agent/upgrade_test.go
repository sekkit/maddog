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
	if !got.ShouldUpgrade || got.TargetModel != "claude" || !strings.Contains(got.Reason, "3 consecutive") || !got.TriggerAdvisor {
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

func TestRunConsultsAdvisorBeforeFrontierAfterRepeatedFailures(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("frontier", testutil.Turn{Text: "frontier used advice"})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	var advisorReqs []AdvisorRequest
	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, TargetModel: "frontier-model"},
		FrontierProvider: frontierProv,
		FrontierTarget:   "frontier-model",
		Advisor: AdvisorConfig{
			MaxUsesPerTurn:     1,
			MaxUsesPerSession:  2,
			MaxContextMessages: 6,
			MaxContextChars:    1000,
		},
		AdvisorRunner: func(_ context.Context, req AdvisorRequest) (string, error) {
			advisorReqs = append(advisorReqs, req)
			if !strings.Contains(req.Context, "write_file") {
				t.Fatalf("advisor context should include failing tool evidence, got:\n%s", req.Context)
			}
			return "1. Stop repeating write_file.\n2. Verify the JSON shape.\nRisks: retrying blindly wastes the turn.", nil
		},
	}, sink)

	if err := a.Run(context.Background(), "make progress"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(advisorReqs) != 1 {
		t.Fatalf("advisor calls = %d, want 1", len(advisorReqs))
	}
	if advisorReqs[0].RemainingTurn != 1 || advisorReqs[0].RemainingSession != 2 {
		t.Fatalf("advisor remaining budget in request = %+v", advisorReqs[0])
	}
	advisorEvents := sink.kinds(event.Advisor)
	if len(advisorEvents) != 1 || !strings.Contains(advisorEvents[0].Advisor.Advice, "Stop repeating") {
		t.Fatalf("advisor events = %+v, want one advice event", advisorEvents)
	}
	reqs := frontierProv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("frontier requests = %d, want 1", len(reqs))
	}
	var sawAdvice bool
	for _, msg := range reqs[0].Messages {
		if msg.Role == provider.RoleUser && strings.Contains(msg.Content, "Advisor guidance") && strings.Contains(msg.Content, "Stop repeating") {
			sawAdvice = true
			break
		}
	}
	if !sawAdvice {
		t.Fatalf("frontier request did not include advisor guidance: %+v", reqs[0].Messages)
	}
}

func TestAdvisorSessionBudgetPreventsRepeatedAutomaticConsults(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("frontier",
		testutil.Turn{Text: "frontier answer one"},
		testutil.Turn{Text: "frontier answer two"},
	)
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	var advisorCalls int
	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
		FrontierProvider: frontierProv,
		FrontierTarget:   "frontier",
		Advisor: AdvisorConfig{
			MaxUsesPerTurn:    1,
			MaxUsesPerSession: 1,
		},
		AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
			advisorCalls++
			return "1. use frontier.\nRisks: low.", nil
		},
	}, sink)

	if err := a.Run(context.Background(), "first failure"); err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	if err := a.Run(context.Background(), "second failure"); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if advisorCalls != 1 {
		t.Fatalf("advisor calls = %d, want session budget to cap at 1", advisorCalls)
	}
	if events := sink.kinds(event.Advisor); len(events) != 1 {
		t.Fatalf("advisor events = %d, want 1", len(events))
	}
}

func TestRunExposesNativeAdvisorWhenConfigured(t *testing.T) {
	mp := testutil.NewMock("anthropic", testutil.Turn{Text: "done"})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		Advisor: AdvisorConfig{
			MaxUsesPerTurn:    2,
			MaxUsesPerSession: 3,
		},
		NativeAdvisor: &provider.NativeAdvisorConfig{
			Model:     "claude-opus-4-6",
			MaxUses:   3,
			MaxTokens: 1024,
		},
	}, event.Discard)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	req := mp.LastRequest()
	if req == nil || req.NativeAdvisor == nil {
		t.Fatalf("request native advisor = %+v", req)
	}
	if req.NativeAdvisor.Model != "claude-opus-4-6" {
		t.Fatalf("native advisor model = %q", req.NativeAdvisor.Model)
	}
	if req.NativeAdvisor.MaxUses != 2 {
		t.Fatalf("native advisor max uses = %d, want per-turn remaining 2", req.NativeAdvisor.MaxUses)
	}
	if req.NativeAdvisor.MaxTokens != 1024 {
		t.Fatalf("native advisor max tokens = %d", req.NativeAdvisor.MaxTokens)
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
