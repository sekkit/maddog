package serve

import (
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestToWire(t *testing.T) {
	t.Run("tool dispatch", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "bash", Args: `{"cmd":"ls"}`, ReadOnly: false}})
		if w.Kind != "tool_dispatch" || w.Tool == nil || w.Tool.Name != "bash" || w.Tool.Args != `{"cmd":"ls"}` {
			t.Errorf("dispatch = %+v / %+v", w, w.Tool)
		}
	})

	t.Run("tool dispatch profile", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
			Name: "task", Args: `{"prompt":"x"}`,
			Profile: &event.Profile{Model: "deepseek-pro", Effort: "max"},
		}})
		if w.Tool == nil || w.Tool.Profile == nil || w.Tool.Profile.Model != "deepseek-pro" || w.Tool.Profile.Effort != "max" {
			t.Errorf("profile = %+v", w.Tool)
		}
	})

	t.Run("tool result duration", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "web_fetch", Output: "ok", DurationMs: 522}})
		if w.Tool == nil || w.Tool.Output != "ok" || w.Tool.DurationMs != 522 {
			t.Errorf("tool result duration = %+v", w.Tool)
		}
	})

	t.Run("usage with cost", func(t *testing.T) {
		w := toWire(event.Event{
			Kind:    event.Usage,
			Usage:   &provider.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CacheHitTokens: 900, CacheMissTokens: 100},
			Pricing: &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2},
			CacheDiagnostics: &event.CacheDiagnostics{
				PrefixChanged:       true,
				PrefixChangeReasons: []string{"log_rewrite"},
				LogRewriteVersion:   1,
			},
		})
		if w.Usage == nil || w.Usage.TotalTokens != 1200 || w.Usage.Cost <= 0 || w.Usage.CostUSD <= 0 || w.Usage.Currency != "¥" {
			t.Errorf("usage = %+v", w.Usage)
		}
		if w.Usage.CacheDiagnostics == nil || w.Usage.CacheDiagnostics.PrefixChangeReasons[0] != "log_rewrite" {
			t.Errorf("cache diagnostics = %+v", w.Usage.CacheDiagnostics)
		}
	})

	t.Run("notice warn", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "truncated"})
		if w.Kind != "notice" || w.Level != "warn" || w.Text != "truncated" {
			t.Errorf("notice = %+v", w)
		}
	})

	t.Run("mcp surface ready", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.MCPSurfaceReady, Text: "tools: prompts ready (2 items)"})
		if w.Kind != "mcp_surface_ready" || w.Level != "info" || w.Text != "tools: prompts ready (2 items)" {
			t.Errorf("mcp surface ready = %+v", w)
		}
	})

	t.Run("runtime events", func(t *testing.T) {
		for _, tc := range []struct {
			ev    event.Event
			kind  string
			level string
		}{
			{event.Event{Kind: event.Upgrade, Level: event.LevelWarn, Text: "frontier"}, "upgrade", "warn"},
			{event.Event{Kind: event.SkillGenerated, Text: "dynamic skill"}, "skill_generated", "info"},
			{event.Event{Kind: event.BudgetExceeded, Level: event.LevelWarn, Text: "budget"}, "budget_exceeded", "warn"},
			{event.Event{Kind: event.SkillPromoted, Text: "promoted"}, "skill_promoted", "info"},
			{event.Event{Kind: event.Advisor, Text: "advisor consulted"}, "advisor", "info"},
		} {
			w := toWire(tc.ev)
			if w.Kind != tc.kind || w.Level != tc.level || w.Text != tc.ev.Text {
				t.Errorf("runtime event = %+v, want kind=%q level=%q text=%q", w, tc.kind, tc.level, tc.ev.Text)
			}
		}
	})

	t.Run("advisor payload", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.Advisor, Level: event.LevelInfo, Text: "advisor consulted", Advisor: event.AdvisorConsultation{
			Reason:               "3 consecutive tool failures",
			Question:             "what now?",
			Advice:               "stop and inspect the failing command",
			UsesThisTurn:         1,
			UsesThisSession:      2,
			RemainingThisTurn:    0,
			RemainingThisSession: 8,
			MaxUsesPerTurn:       1,
			MaxUsesPerSession:    10,
		}})
		if w.Kind != "advisor" || w.Level != "info" || w.Advisor == nil {
			t.Fatalf("advisor wire = %+v", w)
		}
		if w.Advisor.Reason != "3 consecutive tool failures" || w.Advisor.Advice != "stop and inspect the failing command" {
			t.Fatalf("advisor payload = %+v", w.Advisor)
		}
		if w.Advisor.UsesThisTurn != 1 || w.Advisor.RemainingThisSession != 8 || w.Advisor.MaxUsesPerSession != 10 {
			t.Fatalf("advisor budget = %+v", w.Advisor)
		}
	})

	t.Run("approval", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "3", Tool: "bash", Subject: "rm"}})
		if w.Approval == nil || w.Approval.ID != "3" || w.Approval.Tool != "bash" {
			t.Errorf("approval = %+v", w.Approval)
		}
	})

	t.Run("turn done error", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.TurnDone, Err: errors.New("boom")})
		if w.Kind != "turn_done" || w.Err != "boom" {
			t.Errorf("turn_done = %+v", w)
		}
	})

	t.Run("steer", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.Steer, Text: "mid-turn guidance"})
		if w.Kind != "steer" || w.Text != "mid-turn guidance" {
			t.Errorf("steer = %+v", w)
		}
	})
}
