package agent

import (
	"context"
	"testing"

	"maddog/internal/agent/testutil"
	"maddog/internal/event"
	"maddog/internal/provider"
)

func TestUsageEventCarriesDefaultProviderProfile(t *testing.T) {
	prov := testutil.NewMock("icodeeasy/gpt-4o-mini", testutil.Turn{
		Text:  "done",
		Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13},
	})
	sink := &recordSink{}
	a := New(prov, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	usages := sink.kinds(event.Usage)
	if len(usages) != 1 {
		t.Fatalf("usage events = %d, want 1", len(usages))
	}
	profile := usages[0].Profile
	if profile == nil {
		t.Fatal("usage profile = nil, want default provider profile")
	}
	if profile.Role != "default" || profile.Model != "icodeeasy/gpt-4o-mini" {
		t.Fatalf("usage profile = %+v, want default icodeeasy/gpt-4o-mini", profile)
	}
	status := usages[0].ProviderStatus
	if status == nil {
		t.Fatal("usage provider status = nil, want success status snapshot")
	}
	if status.Role != "default" || status.Health != "ok" || status.AuthStatus != "ok" || status.RateLimit != "ok" {
		t.Fatalf("provider status = %+v, want default ok/auth ok/rate ok", status)
	}
}

func TestUsageEventUsesResolvedModelAndEffort(t *testing.T) {
	prov := testutil.NewMock("gateway-instance", testutil.Turn{
		Text:  "done",
		Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13},
	})
	sink := &recordSink{}
	a := New(prov, echoRegistry(), NewSession(""), Options{
		UsageModel:  "icodeeasy/gpt-4o-mini",
		UsageEffort: "high",
	}, sink)

	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	usages := sink.kinds(event.Usage)
	if len(usages) != 1 {
		t.Fatalf("usage events = %d, want 1", len(usages))
	}
	profile := usages[0].Profile
	if profile == nil {
		t.Fatal("usage profile = nil, want resolved provider profile")
	}
	if profile.Role != "default" || profile.Model != "icodeeasy/gpt-4o-mini" || profile.Effort != "high" {
		t.Fatalf("usage profile = %+v, want default icodeeasy/gpt-4o-mini/high", profile)
	}
}

func TestProviderStatusEventClassifiesProviderErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		health    string
		auth      string
		rateLimit string
		balance   string
	}{
		{
			name:      "auth",
			err:       &provider.AuthError{Provider: "openai", KeyEnv: "OPENAI_API_KEY", Status: 401, HasKey: true},
			health:    "auth_error",
			auth:      "auth_error",
			rateLimit: "ok",
		},
		{
			name:      "rate limit",
			err:       &provider.APIError{Provider: "openai", Status: 429, Body: "too many requests"},
			health:    "rate_limited",
			auth:      "ok",
			rateLimit: "rate_limited",
		},
		{
			name:      "structured stream auth",
			err:       &provider.APIError{Provider: "openai", Status: 401, Body: "type=authentication_error: expired token"},
			health:    "auth_error",
			auth:      "auth_error",
			rateLimit: "ok",
		},
		{
			name:      "balance",
			err:       &provider.APIError{Provider: "deepseek", Status: 402, Body: "Insufficient Balance"},
			health:    "balance_error",
			auth:      "ok",
			rateLimit: "ok",
			balance:   "insufficient",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := testutil.NewMock("openai", testutil.Turn{StreamError: tt.err})
			sink := &recordSink{}
			a := New(prov, echoRegistry(), NewSession(""), Options{UsageModel: "openai/gpt-4o"}, sink)

			if err := a.Run(context.Background(), "hello"); err == nil {
				t.Fatal("Run error = nil, want provider error")
			}

			statuses := sink.kinds(event.ProviderStatusUpdate)
			if len(statuses) != 1 {
				t.Fatalf("provider status events = %d, want 1", len(statuses))
			}
			status := statuses[0].ProviderStatus
			if status == nil {
				t.Fatal("provider status payload = nil")
			}
			if status.Role != "default" || status.Health != tt.health || status.AuthStatus != tt.auth || status.RateLimit != tt.rateLimit {
				t.Fatalf("provider status = %+v, want role default health=%s auth=%s rate=%s", status, tt.health, tt.auth, tt.rateLimit)
			}
			if tt.balance != "" && status.BalanceStatus != tt.balance {
				t.Fatalf("balance status = %q, want %q", status.BalanceStatus, tt.balance)
			}
			if status.LastError == "" {
				t.Fatal("provider status LastError should describe the failure")
			}
		})
	}
}

func TestProviderStatusEventSkipsContextCancellation(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			prov := testutil.NewMock("openai", testutil.Turn{StreamError: err})
			sink := &recordSink{}
			a := New(prov, echoRegistry(), NewSession(""), Options{UsageModel: "openai/gpt-4o"}, sink)

			if runErr := a.Run(context.Background(), "hello"); runErr == nil {
				t.Fatal("Run error = nil, want cancellation/deadline error")
			}

			if statuses := sink.kinds(event.ProviderStatusUpdate); len(statuses) != 0 {
				t.Fatalf("provider status events = %d, want none for local cancellation/deadline", len(statuses))
			}
		})
	}
}

func TestUsageEventCarriesFrontierProviderProfile(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("anthropic", testutil.Turn{
		Text:  "frontier answer",
		Usage: &provider.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})
	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, TargetModel: "anthropic/claude-3-5-sonnet"},
		FrontierProvider: frontierProv,
		FrontierTarget:   "anthropic/claude-3-5-sonnet",
	}, sink)

	if err := a.Run(context.Background(), "recover with frontier"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	usages := sink.kinds(event.Usage)
	if len(usages) != 1 {
		t.Fatalf("usage events = %d, want 1", len(usages))
	}
	profile := usages[0].Profile
	if profile == nil {
		t.Fatal("usage profile = nil, want frontier provider profile")
	}
	if profile.Role != "frontier" || profile.Model != "anthropic/claude-3-5-sonnet" {
		t.Fatalf("usage profile = %+v, want frontier anthropic/claude-3-5-sonnet", profile)
	}
}

func TestUsageEventCarriesFrontierBudgetSnapshot(t *testing.T) {
	defaultProv := testutil.NewMock("default",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{}`}}},
	)
	frontierProv := testutil.NewMock("anthropic", testutil.Turn{
		Text:  "frontier answer",
		Usage: &provider.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	})
	sink := &recordSink{}
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})
	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 2, BudgetLimit: 10, TargetModel: "anthropic/claude-3-5-sonnet"},
		FrontierProvider: frontierProv,
		FrontierTarget:   "anthropic/claude-3-5-sonnet",
	}, sink)

	if err := a.Run(context.Background(), "recover with budget visibility"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	usages := sink.kinds(event.Usage)
	if len(usages) != 1 {
		t.Fatalf("usage events = %d, want 1", len(usages))
	}
	profile := usages[0].Profile
	if profile == nil {
		t.Fatal("usage profile = nil, want frontier provider profile")
	}
	if profile.BudgetUsed != 7 || profile.BudgetLimit != 10 || profile.BudgetRemaining != 3 {
		t.Fatalf("usage profile budget = used:%d limit:%d remaining:%d, want 7/10 remaining 3", profile.BudgetUsed, profile.BudgetLimit, profile.BudgetRemaining)
	}
}
