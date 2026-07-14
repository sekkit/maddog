package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"maddog/internal/event"
	"maddog/internal/provider"
)

func TestWireEventTabPreservesSharedRetryingFields(t *testing.T) {
	w := toWireTab(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10}, "tab-1")
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if s := string(b); !strings.Contains(s, `"retryAttempt":3`) || !strings.Contains(s, `"retryMax":10`) {
		t.Errorf("retrying JSON = %s", s)
	}
}

func TestToWireToolDispatch(t *testing.T) {
	e := event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "1", Name: "bash", Args: `{"c":"echo"}`, ReadOnly: false}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Name != "bash" || w.Tool.Args != `{"c":"echo"}` {
		t.Errorf("tool dispatch = %+v", w.Tool)
	}
}

func TestToWireToolDispatchProfile(t *testing.T) {
	e := event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "1", Name: "task", Args: `{"prompt":"x"}`,
		Profile: &event.Profile{Model: "deepseek-pro", Effort: "max"},
	}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Profile == nil || w.Tool.Profile.Model != "deepseek-pro" || w.Tool.Profile.Effort != "max" {
		t.Errorf("tool profile = %+v", w.Tool)
	}
}

func TestToWireToolResult(t *testing.T) {
	e := event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "1", Output: "ok", Truncated: true, DurationMs: 522,
		Compression: &event.Compression{
			RawRef:           "raw://tool/1",
			Route:            "profile",
			Profile:          "go-test",
			Quality:          "degraded",
			QualityReason:    "heuristic text profile",
			UnparsedLines:    2,
			UnparsedSamples:  []string{"unknown one", "unknown two"},
			Lossy:            true,
			OmittedLines:     42,
			Strategy:         "shell-error-first",
			Summary:          "bash output compressed",
			RawChars:         1000,
			CompressedChars:  120,
			SavedChars:       880,
			RawTokens:        250,
			CompressedTokens: 30,
			SavedTokens:      220,
		},
	}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Output != "ok" || !w.Tool.Truncated || w.Tool.DurationMs != 522 {
		t.Errorf("tool result = %+v", w.Tool)
	}
	if w.Tool.Compression == nil || w.Tool.Compression.RawRef != "raw://tool/1" || w.Tool.Compression.SavedTokens != 220 ||
		w.Tool.Compression.Route != "profile" || w.Tool.Compression.Profile != "go-test" ||
		w.Tool.Compression.Quality != "degraded" || w.Tool.Compression.UnparsedLines != 2 || len(w.Tool.Compression.UnparsedSamples) != 2 ||
		!w.Tool.Compression.Lossy || w.Tool.Compression.OmittedLines != 42 {
		t.Errorf("tool compression = %+v", w.Tool.Compression)
	}
}

func TestToWireToolProgress(t *testing.T) {
	e := event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "1", Output: "chunk"}}
	w := toWire(e)
	if w.Kind != "tool_progress" || w.Tool == nil || w.Tool.Output != "chunk" {
		t.Errorf("tool progress = kind:%q tool:%+v", w.Kind, w.Tool)
	}
}

func TestToWireUsage(t *testing.T) {
	e := event.Event{
		Kind:    event.Usage,
		Usage:   &provider.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, CacheHitTokens: 80, CacheMissTokens: 20},
		Profile: &event.Profile{Role: "default", Model: "icodeeasy/gpt-4o-mini", BudgetUsed: 2, BudgetLimit: 10, BudgetRemaining: 8},
		ProviderStatus: &event.ProviderStatus{
			Role:       "default",
			Health:     "ok",
			AuthStatus: "ok",
			RateLimit:  "ok",
		},
		SessionHit:  800,
		SessionMiss: 200,
	}
	w := toWireTab(e, "tab-1")
	if w.Kind != "usage" || w.TabID != "tab-1" {
		t.Fatalf("tab wire = %+v", w)
	}
	if w.SessionHitTokens != 800 || w.SessionMissTokens != 200 {
		t.Fatalf("session tokens = hit:%d miss:%d", w.SessionHitTokens, w.SessionMissTokens)
	}
	if w.Usage.Profile == nil || w.Usage.Profile.Role != "default" || w.Usage.Profile.Model != "icodeeasy/gpt-4o-mini" {
		t.Errorf("usage profile = %+v", w.Usage.Profile)
	}
	if w.Usage.Profile == nil || w.Usage.Profile.BudgetUsed != 2 || w.Usage.Profile.BudgetLimit != 10 || w.Usage.Profile.BudgetRemaining != 8 {
		t.Errorf("usage profile budget = %+v", w.Usage.Profile)
	}
	if w.Usage.ProviderStatus == nil || w.Usage.ProviderStatus.Health != "ok" || w.Usage.ProviderStatus.AuthStatus != "ok" || w.Usage.ProviderStatus.RateLimit != "ok" {
		t.Errorf("usage provider status = %+v", w.Usage.ProviderStatus)
	}
}

func TestToWireProviderStatus(t *testing.T) {
	e := event.Event{Kind: event.ProviderStatusUpdate, ProviderStatus: &event.ProviderStatus{
		Role:          "default",
		Health:        "auth_error",
		AuthStatus:    "auth_error",
		RateLimit:     "ok",
		BalanceStatus: "unknown",
		LastError:     "authentication failed",
	}}
	w := toWire(e)
	if w.Kind != "provider_status" || w.ProviderStatus == nil {
		t.Fatalf("provider status wire = %+v", w)
	}
	if w.ProviderStatus.Health != "auth_error" || w.ProviderStatus.AuthStatus != "auth_error" || w.ProviderStatus.LastError == "" {
		t.Fatalf("provider status payload = %+v", w.ProviderStatus)
	}
}

func TestWireEventTabPreservesMaddogRuntimeEvents(t *testing.T) {
	w := toWireTab(event.Event{
		Kind:  event.Advisor,
		Level: event.LevelWarn,
		Text:  "advisor consulted",
		Advisor: event.AdvisorConsultation{
			Advice:            "inspect the failing command",
			UsesThisSession:   2,
			MaxUsesPerSession: 10,
		},
	}, "tab-1")
	if w.Kind != "advisor" || w.Level != "warn" || w.Advisor == nil {
		t.Fatalf("advisor tab wire = %+v", w)
	}
}

func TestToWireUsageCost(t *testing.T) {
	e := event.Event{
		Kind:    event.Usage,
		Usage:   &provider.Usage{PromptTokens: 1_000_000},
		Pricing: &provider.Pricing{Input: 1},
	}
	w := toWire(e)
	if w.Usage == nil || w.Usage.Cost != 1.0 || w.Usage.CostUSD != 1.0 {
		t.Errorf("cost = %+v, want cost and compat costUsd of 1.0", w.Usage)
	}
	if w.Usage.Currency != "¥" {
		t.Errorf("currency = %q, want ¥", w.Usage.Currency)
	}
}

func TestToWireApprovalRequest(t *testing.T) {
	e := event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "42", Tool: "bash", Subject: "rm"}}
	w := toWire(e)
	if w.Approval == nil || w.Approval.ID != "42" || w.Approval.Tool != "bash" {
		t.Errorf("approval = %+v", w.Approval)
	}
}

func TestToWireAskRequest(t *testing.T) {
	e := event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID:        "ask-1",
		Questions: []event.AskQuestion{{ID: "q1", Header: "Pick", Prompt: "Choose one", Options: []event.AskOption{{Label: "A"}, {Label: "B"}}, Multi: false}},
	}}
	w := toWire(e)
	if w.Ask == nil || w.Ask.ID != "ask-1" {
		t.Errorf("ask = %+v", w.Ask)
	}
	if len(w.Ask.Questions) != 1 || len(w.Ask.Questions[0].Options) != 2 {
		t.Errorf("questions/options = %+v", w.Ask.Questions)
	}
}

func TestToWireTurnDoneWithError(t *testing.T) {
	e := event.Event{Kind: event.TurnDone, Err: errors.New("boom")}
	w := toWire(e)
	if w.Kind != "turn_done" || w.Err != "boom" {
		t.Errorf("turn_done error = %+v", w)
	}
}

func TestToWireTurnDoneNoError(t *testing.T) {
	e := event.Event{Kind: event.TurnDone}
	w := toWire(e)
	if w.Err != "" {
		t.Errorf("turn_done no-error should have empty err, got %q", w.Err)
	}
}

// --- kindNames completeness ---

func TestToWireSteer(t *testing.T) {
	e := event.Event{Kind: event.Steer, Text: "please use smaller diffs"}
	w := toWire(e)
	if w.Kind != "steer" || w.Text != "please use smaller diffs" {
		t.Errorf("steer wire = %+v", w)
	}
}

func TestKindNamesComplete(t *testing.T) {
	// Advisor is the last Kind; every value through it must have a wire name,
	// or toWire emits kind:"" and the frontend reducer falls through to undefined.
	for k := event.Kind(0); k <= event.ProviderStatusUpdate; k++ {
		if kindNames[k] == "" {
			t.Errorf("kind %d has no wire name — toWire would emit kind:\"\"", k)
		}
	}
}

func TestToWireRuntimeEvents(t *testing.T) {
	tests := []struct {
		name  string
		event event.Event
		kind  string
		level string
	}{
		{name: "mcp surface ready", event: event.Event{Kind: event.MCPSurfaceReady, Text: "server: prompts ready (2 items)"}, kind: "mcp_surface_ready", level: "info"},
		{name: "upgrade", event: event.Event{Kind: event.Upgrade, Level: event.LevelWarn, Text: "frontier route"}, kind: "upgrade", level: "warn"},
		{name: "skill generated", event: event.Event{Kind: event.SkillGenerated, Level: event.LevelInfo, Text: "generated skill"}, kind: "skill_generated", level: "info"},
		{name: "budget exceeded", event: event.Event{Kind: event.BudgetExceeded, Level: event.LevelWarn, Text: "budget hit"}, kind: "budget_exceeded", level: "warn"},
		{name: "skill promoted", event: event.Event{Kind: event.SkillPromoted, Level: event.LevelInfo, Text: "promoted skill"}, kind: "skill_promoted", level: "info"},
		{name: "advisor", event: event.Event{Kind: event.Advisor, Level: event.LevelInfo, Text: "advisor consulted"}, kind: "advisor", level: "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := toWire(tt.event)
			if w.Kind != tt.kind || w.Level != tt.level || w.Text != tt.event.Text {
				t.Fatalf("runtime wire = %+v, want kind=%q level=%q text=%q", w, tt.kind, tt.level, tt.event.Text)
			}
		})
	}
}

func TestToWireAdvisor(t *testing.T) {
	e := event.Event{Kind: event.Advisor, Level: event.LevelInfo, Text: "advisor consulted", Advisor: event.AdvisorConsultation{
		Reason:               "3 consecutive tool failures",
		Question:             "what now?",
		Advice:               "stop and inspect the failing command",
		UsesThisTurn:         1,
		UsesThisSession:      2,
		RemainingThisTurn:    0,
		RemainingThisSession: 8,
		MaxUsesPerTurn:       1,
		MaxUsesPerSession:    10,
	}}
	w := toWire(e)
	if w.Kind != "advisor" || w.Level != "info" || w.Advisor == nil {
		t.Fatalf("advisor wire = %+v", w)
	}
	if w.Advisor.Question != "what now?" || w.Advisor.Advice != "stop and inspect the failing command" {
		t.Fatalf("advisor payload = %+v", w.Advisor)
	}
}
