package serve

import (
	"errors"
	"testing"

	"maddog/internal/event"
	"maddog/internal/loop"
	"maddog/internal/provider"
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
		w := toWire(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "web_fetch", Output: "ok", DurationMs: 522, Compression: &event.ToolCompression{
			Compressed: true, Strategy: "deterministic_head_tail_errors", RawRef: "tool://web/raw", OriginalBytes: 100, CompressedBytes: 45, SavedBytes: 55,
		}}})
		if w.Tool == nil || w.Tool.Output != "ok" || w.Tool.DurationMs != 522 {
			t.Errorf("tool result duration = %+v", w.Tool)
		}
		if w.Tool.Compression == nil || w.Tool.Compression.RawRef != "tool://web/raw" || w.Tool.Compression.SavedBytes != 55 {
			t.Errorf("tool compression = %+v", w.Tool.Compression)
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

	t.Run("skill generated candidate payload", func(t *testing.T) {
		w := toWire(event.Event{
			Kind:  event.SkillGenerated,
			Level: event.LevelInfo,
			Text:  "generated pending skill candidate dynamic-docs",
			SkillCandidate: &event.SkillCandidateSnapshot{
				SkillName:   "dynamic-docs",
				BundleID:    "bundle-123",
				CandidateID: "cand-456",
				Status:      "pending",
			},
		})
		if w.Kind != "skill_generated" || w.SkillCandidate == nil {
			t.Fatalf("skill generated wire = %+v", w)
		}
		if w.SkillCandidate.BundleID != "bundle-123" || w.SkillCandidate.CandidateID != "cand-456" {
			t.Fatalf("candidate payload = %+v", w.SkillCandidate)
		}
	})

	t.Run("readiness payload", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.Readiness, Readiness: &loop.ReadinessResult{
			Status:     loop.ReadinessBlocked,
			TemplateID: "coding-task",
			Blockers:   []string{"credential unavailable"},
			Checks: []loop.ReadinessCheck{{
				ID:            "credential_available",
				Status:        loop.CheckBlocked,
				CredentialEnv: "OPENAI_API_KEY",
			}},
		}})
		if w.Kind != "readiness" || w.Level != "warn" || w.Readiness == nil {
			t.Fatalf("readiness wire = %+v", w)
		}
		if w.Readiness.Status != loop.ReadinessBlocked || w.Readiness.Checks[0].CredentialEnv != "OPENAI_API_KEY" {
			t.Fatalf("readiness payload = %+v", w.Readiness)
		}
	})

	t.Run("provider status payload", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ProviderStatus, Level: event.LevelInfo, ProviderStatus: event.ProviderStatusSnapshot{
			Role:                  "frontier",
			Provider:              "anthropic-frontier",
			Model:                 "anthropic-frontier/claude-sonnet-4",
			Status:                "active",
			UpgradeReason:         "2 consecutive tool failures",
			RequestCount:          1,
			PromptTokens:          50,
			CompletionTokens:      7,
			TotalTokens:           57,
			Cost:                  0.001,
			Currency:              "$",
			BudgetUsedTokens:      7,
			BudgetLimitTokens:     10,
			BudgetRemainingTokens: 3,
		}})
		if w.Kind != "provider_status" || w.Level != "info" || w.ProviderStatus == nil {
			t.Fatalf("provider status wire = %+v", w)
		}
		if w.ProviderStatus.Role != "frontier" || w.ProviderStatus.Provider != "anthropic-frontier" || w.ProviderStatus.Model != "anthropic-frontier/claude-sonnet-4" {
			t.Fatalf("provider status identity = %+v", w.ProviderStatus)
		}
		if w.ProviderStatus.TotalTokens != 57 || w.ProviderStatus.Cost != 0.001 || w.ProviderStatus.Currency != "$" {
			t.Fatalf("provider status usage/cost = %+v", w.ProviderStatus)
		}
		if w.ProviderStatus.BudgetRemainingTokens != 3 || w.ProviderStatus.UpgradeReason == "" {
			t.Fatalf("provider status budget/reason = %+v", w.ProviderStatus)
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

	t.Run("maker checker and human gate payloads", func(t *testing.T) {
		mc := toWire(event.Event{Kind: event.MakerChecker, MakerChecker: &loop.MakerCheckerResult{
			Mode:        loop.MakerCheckerEnforcedBeforeDone,
			Verdict:     loop.CheckerChangesRequested,
			CanComplete: false,
		}})
		if mc.Kind != "maker_checker" || mc.MakerChecker == nil || mc.MakerChecker.Verdict != loop.CheckerChangesRequested {
			t.Fatalf("maker checker wire = %+v", mc)
		}
		gate := toWire(event.Event{Kind: event.HumanGate, HumanGate: &loop.HumanGateResult{
			Kind:     loop.HumanGateGitPush,
			Required: true,
			Status:   "needs_human",
		}})
		if gate.Kind != "human_gate" || gate.HumanGate == nil || gate.HumanGate.Kind != loop.HumanGateGitPush {
			t.Fatalf("human gate wire = %+v", gate)
		}
	})

	t.Run("run report payload", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.RunReportReady, Level: event.LevelInfo, RunReport: &loop.RunReport{
			RunID:       "run-1",
			LoopID:      "coding-task",
			TemplateID:  "coding-task",
			Status:      "completed",
			FinalStatus: "completed",
			Path:        "C:/Users/Sekkit/AppData/Roaming/maddog/runs/run-1/run.jsonl",
			ReportPath:  "C:/Users/Sekkit/AppData/Roaming/maddog/runs/run-1/report.json",
			Events:      4,
			Phases:      []loop.RunReportPhase{{ID: "readiness", Status: "completed"}},
			Models:      []loop.RunReportModel{{Role: "frontier", Provider: "openai-official", Model: "gpt-5", TotalTokens: 1200, UpgradeReason: "low confidence"}},
			Budget:      loop.RunReportBudget{UsedTokens: 1200, LimitTokens: 2000, RemainingTokens: 800, Cost: 0.42, Currency: "$"},
			Readiness:   &loop.ReadinessResult{Status: loop.ReadinessReady, Score: 100, TemplateID: "coding-task"},
			Checker:     &loop.MakerCheckerResult{Mode: loop.MakerCheckerEnforcedBeforeDone, Verdict: loop.CheckerApproved, CanComplete: true},
			HumanGate:   &loop.HumanGateResult{Kind: loop.HumanGateGitPush, Required: true, Status: "pending"},
		}})
		if w.Kind != "run_report_ready" || w.Level != "info" || w.RunReport == nil {
			t.Fatalf("run report wire = %+v", w)
		}
		if w.RunReport.RunID != "run-1" || w.RunReport.Status != "completed" || w.RunReport.Events != 4 {
			t.Fatalf("run report payload = %+v", w.RunReport)
		}
		if w.RunReport.ReportPath == "" || len(w.RunReport.Models) != 1 || w.RunReport.Budget.RemainingTokens != 800 {
			t.Fatalf("run report detail missing = %+v", w.RunReport)
		}
		if w.RunReport.Readiness == nil || w.RunReport.Checker == nil || w.RunReport.HumanGate == nil {
			t.Fatalf("run report gates missing = %+v", w.RunReport)
		}
	})
}
