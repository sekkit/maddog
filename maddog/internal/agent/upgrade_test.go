package agent

import (
	"context"
	"strings"
	"testing"

	"maddog/internal/agent/testutil"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
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

func TestRunEmitsProviderStatusForDefaultAndFrontierUsage(t *testing.T) {
	defaultProv := testutil.NewMock("default-provider",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}},
			Usage:     &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheMissTokens: 100},
		},
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}},
			Usage:     &provider.Usage{PromptTokens: 80, CompletionTokens: 10, TotalTokens: 90, CacheMissTokens: 80},
		},
	)
	frontierProv := testutil.NewMock("frontier-provider", testutil.Turn{
		Text:  "frontier answer",
		Usage: &provider.Usage{PromptTokens: 50, CompletionTokens: 7, TotalTokens: 57, CacheMissTokens: 50},
	})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	a := New(defaultProv, reg, NewSession(""), Options{
		Pricing:          &provider.Pricing{Input: 2, Output: 10, Currency: "USD"},
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, BudgetLimit: 10, TargetModel: "frontier-model"},
		FrontierProvider: frontierProv,
		FrontierPricing:  &provider.Pricing{Input: 4, Output: 20, Currency: "USD"},
		FrontierTarget:   "frontier-model",
	}, sink)

	if err := a.Run(context.Background(), "try route with observable providers"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	statuses := sink.kinds(event.ProviderStatus)
	if len(statuses) < 3 {
		t.Fatalf("provider status events = %d, want at least default usage, upgrade, frontier usage: %+v", len(statuses), statuses)
	}
	var defaultStatus, frontierStatus *event.ProviderStatusSnapshot
	for i := range statuses {
		status := statuses[i].ProviderStatus
		switch status.Role {
		case "default":
			defaultStatus = &status
		case "frontier":
			frontierStatus = &status
		}
		if strings.Contains(status.Provider, "sk-") || strings.Contains(status.Model, "sk-") || strings.Contains(status.UpgradeReason, "sk-") {
			t.Fatalf("provider status leaked credential-like text: %+v", status)
		}
	}
	if defaultStatus == nil {
		t.Fatalf("missing default provider status: %+v", statuses)
	}
	if defaultStatus.Provider != "default-provider" || defaultStatus.RequestCount != 2 || defaultStatus.TotalTokens != 210 {
		t.Fatalf("default status = %+v, want provider default-provider with 2 requests and 210 tokens", defaultStatus)
	}
	if defaultStatus.Cost <= 0 || defaultStatus.Currency != "$" {
		t.Fatalf("default cost = %f %q, want positive $", defaultStatus.Cost, defaultStatus.Currency)
	}
	if frontierStatus == nil {
		t.Fatalf("missing frontier provider status: %+v", statuses)
	}
	if frontierStatus.Provider != "frontier-provider" || frontierStatus.Model != "frontier-model" {
		t.Fatalf("frontier identity = provider:%q model:%q", frontierStatus.Provider, frontierStatus.Model)
	}
	if !strings.Contains(frontierStatus.UpgradeReason, "2 consecutive") {
		t.Fatalf("frontier upgrade reason = %q, want policy reason", frontierStatus.UpgradeReason)
	}
	if frontierStatus.BudgetLimitTokens != 10 || frontierStatus.BudgetUsedTokens != 7 || frontierStatus.BudgetRemainingTokens != 3 {
		t.Fatalf("frontier budget = used:%d limit:%d remaining:%d, want 7/10/3",
			frontierStatus.BudgetUsedTokens, frontierStatus.BudgetLimitTokens, frontierStatus.BudgetRemainingTokens)
	}
}

func TestRunEmitsProviderStatusForConfiguredSmallRole(t *testing.T) {
	smallProv := testutil.NewMock("small-provider", testutil.Turn{
		Text:  "small answer",
		Usage: &provider.Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15, CacheMissTokens: 12},
	})
	sink := &recordSink{}
	a := New(smallProv, echoRegistry(), NewSession(""), Options{
		ProviderRole: "small",
		Pricing:      &provider.Pricing{Input: 1, Output: 2, Currency: "USD"},
	}, sink)

	if err := a.Run(context.Background(), "delegate this"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	statuses := sink.kinds(event.ProviderStatus)
	if len(statuses) != 1 {
		t.Fatalf("provider status events = %d, want 1: %+v", len(statuses), statuses)
	}
	status := statuses[0].ProviderStatus
	if status.Role != "small" || status.Provider != "small-provider" || status.TotalTokens != 15 || status.Cost <= 0 {
		t.Fatalf("small provider status = %+v", status)
	}
}

func TestAdvisorConsultationEmitsProviderStatus(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("frontier", testutil.Turn{Text: "frontier used advice"})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})

	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, TargetModel: "frontier-model"},
		FrontierProvider: frontierProv,
		FrontierTarget:   "frontier-model",
		Advisor: AdvisorConfig{
			MaxUsesPerTurn:    1,
			MaxUsesPerSession: 2,
		},
		AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
			return "Inspect the failing write_file arguments first.", nil
		},
	}, sink)

	if err := a.Run(context.Background(), "make progress"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var advisorStatus *event.ProviderStatusSnapshot
	for _, e := range sink.kinds(event.ProviderStatus) {
		status := e.ProviderStatus
		if status.Role == "advisor" {
			advisorStatus = &status
			break
		}
	}
	if advisorStatus == nil {
		t.Fatalf("missing advisor provider status: %+v", sink.kinds(event.ProviderStatus))
	}
	if advisorStatus.Provider != "advisor" || advisorStatus.RequestCount != 1 || advisorStatus.Status != "active" {
		t.Fatalf("advisor provider status = %+v", advisorStatus)
	}
}
