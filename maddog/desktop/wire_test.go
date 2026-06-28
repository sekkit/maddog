package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"maddog/internal/event"
	"maddog/internal/loop"
	"maddog/internal/provider"
)

// --- toWire ---

func TestToWireText(t *testing.T) {
	e := event.Event{Kind: event.Text, Text: "hello"}
	w := toWire(e)
	if w.Kind != "text" || w.Text != "hello" {
		t.Errorf("text wire = %+v", w)
	}
}

func TestToWireReasoning(t *testing.T) {
	e := event.Event{Kind: event.Reasoning, Text: "thinking..."}
	w := toWire(e)
	if w.Kind != "reasoning" || w.Text != "thinking..." {
		t.Errorf("reasoning wire = %+v", w)
	}
}

func TestToWireNoticeInfo(t *testing.T) {
	e := event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "info"}
	w := toWire(e)
	if w.Kind != "notice" || w.Level != "info" {
		t.Errorf("notice info = %+v", w)
	}
}

func TestToWireNoticeWarn(t *testing.T) {
	e := event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "warn"}
	w := toWire(e)
	if w.Level != "warn" {
		t.Errorf("notice warn level = %q", w.Level)
	}
}

func TestToWireRetrying(t *testing.T) {
	e := event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10}
	w := toWire(e)
	if w.Kind != "retrying" || w.RetryAttempt != 3 || w.RetryMax != 10 {
		t.Errorf("retrying wire = %+v", w)
	}
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
	e := event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "1", Output: "ok", Truncated: true, DurationMs: 522, Compression: &event.ToolCompression{
		Compressed: true, Strategy: "deterministic_head_tail_errors", RawRef: "tool://1/raw", OriginalBytes: 100, CompressedBytes: 40, SavedBytes: 60,
	}}}
	w := toWire(e)
	if w.Tool == nil || w.Tool.Output != "ok" || !w.Tool.Truncated || w.Tool.DurationMs != 522 {
		t.Errorf("tool result = %+v", w.Tool)
	}
	if w.Tool.Compression == nil || !w.Tool.Compression.Compressed || w.Tool.Compression.RawRef != "tool://1/raw" || w.Tool.Compression.SavedBytes != 60 {
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
		Kind:        event.Usage,
		Usage:       &provider.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, CacheHitTokens: 80, CacheMissTokens: 20},
		SessionHit:  800,
		SessionMiss: 200,
	}
	w := toWire(e)
	if w.Usage == nil || w.Usage.PromptTokens != 100 || w.Usage.TotalTokens != 150 {
		t.Errorf("usage = %+v", w.Usage)
	}
	if w.Usage.SessionCacheHitTokens != 800 || w.Usage.SessionCacheMissTokens != 200 {
		t.Errorf("session cache = hit:%d miss:%d", w.Usage.SessionCacheHitTokens, w.Usage.SessionCacheMissTokens)
	}
}

func TestToWireUsageWithPricing(t *testing.T) {
	e := event.Event{
		Kind:    event.Usage,
		Usage:   &provider.Usage{CacheHitTokens: 1_000_000, CacheMissTokens: 0, CompletionTokens: 0},
		Pricing: &provider.Pricing{CacheHit: 1.0, Input: 2.0, Output: 10.0},
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
	// HumanGate is the last Kind; every value through it must have a wire name,
	// or toWire emits kind:"" and the frontend reducer falls through to undefined.
	for k := event.Kind(0); k <= event.HumanGate; k++ {
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

func TestToWireSkillGeneratedCandidatePayload(t *testing.T) {
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
	if w.SkillCandidate.SkillName != "dynamic-docs" || w.SkillCandidate.BundleID != "bundle-123" || w.SkillCandidate.CandidateID != "cand-456" {
		t.Fatalf("candidate payload = %+v", w.SkillCandidate)
	}
}

func TestToWireReadiness(t *testing.T) {
	e := event.Event{Kind: event.Readiness, Readiness: &loop.ReadinessResult{
		Status:     loop.ReadinessBlocked,
		TemplateID: "coding-task",
		Blockers:   []string{"credential unavailable"},
		Checks: []loop.ReadinessCheck{{
			ID:            "credential_available",
			Status:        loop.CheckBlocked,
			CredentialEnv: "OPENAI_API_KEY",
		}},
	}}
	w := toWire(e)
	if w.Kind != "readiness" || w.Level != "warn" || w.Readiness == nil {
		t.Fatalf("readiness wire = %+v", w)
	}
	if w.Readiness.Status != loop.ReadinessBlocked || w.Readiness.Checks[0].CredentialEnv != "OPENAI_API_KEY" {
		t.Fatalf("readiness payload = %+v", w.Readiness)
	}
}

func TestToWireMakerCheckerAndHumanGate(t *testing.T) {
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
}

func TestToWireProviderStatus(t *testing.T) {
	e := event.Event{Kind: event.ProviderStatus, Level: event.LevelInfo, ProviderStatus: event.ProviderStatusSnapshot{
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
	}}
	w := toWire(e)
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
}

func TestToWireRunReportReady(t *testing.T) {
	e := event.Event{Kind: event.RunReportReady, Level: event.LevelInfo, RunReport: &loop.RunReport{
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
	}}
	w := toWire(e)
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
	if w.Advisor.UsesThisSession != 2 || w.Advisor.RemainingThisSession != 8 {
		t.Fatalf("advisor budget = %+v", w.Advisor)
	}
}

// --- wireEvent JSON round-trip ---

func TestWireEventJSON(t *testing.T) {
	w := wireEvent{Kind: "text", Text: "hello"}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded wireEvent
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Kind != "text" || decoded.Text != "hello" {
		t.Errorf("round-trip = %+v", decoded)
	}
}
