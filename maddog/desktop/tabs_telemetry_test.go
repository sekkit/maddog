package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/control"
	"maddog/internal/event"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type usageProvider struct {
	usage *provider.Usage
}

func (p usageProvider) Name() string { return "usage" }

func (p usageProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: p.usage}
	close(ch)
	return ch, nil
}

func TestTelemetryLoadsLegacyReadFileArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	if err := os.WriteFile(path, []byte(`[{"path":"README.md","turn":2,"time":1000}]`), 0o644); err != nil {
		t.Fatalf("write legacy telemetry: %v", err)
	}

	got := loadTelemetry(path)
	if len(got.ReadFiles) != 1 || got.ReadFiles[0].Path != "README.md" {
		t.Fatalf("legacy read files = %+v", got.ReadFiles)
	}
	if got.Usage.RequestCount != 0 {
		t.Fatalf("legacy usage request count = %d, want 0", got.Usage.RequestCount)
	}
}

func TestTelemetrySnapshotHasDataForCompressionOnlySnapshot(t *testing.T) {
	snapshot := tabTelemetrySnapshot{Version: 2, Usage: sessionUsageStats{
		CompressionEvents:      1,
		CompressionRawTokens:   1000,
		CompressionSavedTokens: 750,
	}}
	if !telemetrySnapshotHasData(snapshot) {
		t.Fatal("compression-only telemetry snapshot should be restored")
	}
}

func TestWorkspaceTabAggregatesSessionUsageTelemetry(t *testing.T) {
	tab := &WorkspaceTab{}
	start := time.Now().Add(-2 * time.Second).UnixMilli()
	tab.recordTurnStarted(start)
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 100, CompletionTokens: 40, TotalTokens: 140, CacheHitTokens: 70, CacheMissTokens: 30, ReasoningTokens: 10},
		UsageSource: event.UsageSourceSubagent,
		SessionHit:  70,
		SessionMiss: 30,
		Pricing:     &provider.Pricing{CacheHit: 1, Input: 2, Output: 3, Currency: "¥"},
	})
	tab.recordTurnDone(start + 1500)

	got := tab.telemetrySnapshot().Usage
	if got.RequestCount != 1 || got.PromptTokens != 100 || got.CompletionTokens != 40 || got.TotalTokens != 140 || got.ReasoningTokens != 10 {
		t.Fatalf("usage tokens = %+v", got)
	}
	if got.CacheHitTokens != 70 || got.CacheMissTokens != 30 {
		t.Fatalf("cache tokens = hit %d miss %d", got.CacheHitTokens, got.CacheMissTokens)
	}
	if got.ElapsedMs != 1500 {
		t.Fatalf("elapsed = %d, want 1500", got.ElapsedMs)
	}
	if got.SessionCost <= 0 || got.SessionCurrency != "¥" {
		t.Fatalf("cost = %f %q, want positive ¥", got.SessionCost, got.SessionCurrency)
	}
	if got.Sources[event.UsageSourceSubagent].SessionCost <= 0 || got.Sources[event.UsageSourceSubagent].RequestCount != 1 {
		t.Fatalf("subagent source stats = %+v, want one costed request", got.Sources[event.UsageSourceSubagent])
	}

	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	context := app.ContextUsageForTab("tab")
	if context.SessionTokens != 140 {
		t.Fatalf("context usage session tokens = %d, want 140", context.SessionTokens)
	}
	if context.SessionCost <= 0 || context.SessionCurrency != "¥" {
		t.Fatalf("context usage cost = %f %q, want positive ¥", context.SessionCost, context.SessionCurrency)
	}
	if context.CacheHitTokens != 70 || context.CacheMissTokens != 30 {
		t.Fatalf("context usage cache tokens = hit %d miss %d, want 70/30", context.CacheHitTokens, context.CacheMissTokens)
	}
	panel := app.ContextPanel("tab")
	if panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want 140", panel.TotalTokens)
	}
	if panel.SessionCompletionTokens != 40 {
		t.Fatalf("context panel session completion tokens = %d, want 40", panel.SessionCompletionTokens)
	}
	if panel.SessionCacheHitTokens != 70 || panel.SessionCacheMissTokens != 30 {
		t.Fatalf("context panel session cache tokens = hit %d miss %d, want 70/30", panel.SessionCacheHitTokens, panel.SessionCacheMissTokens)
	}
	if panel.Sources[event.UsageSourceSubagent].CompletionTokens != 40 {
		t.Fatalf("context panel source stats = %+v, want subagent completion tokens", panel.Sources)
	}
}

func TestContextPanelIncludesToolCompressionTelemetry(t *testing.T) {
	tab := &WorkspaceTab{ID: "tab"}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	sink := &tabEventSink{tabID: "tab", app: app}

	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID:   "tool-1",
		Name: "bash",
		Compression: &event.Compression{
			Strategy:         "go-test-failure",
			RawChars:         4000,
			CompressedChars:  900,
			SavedChars:       3100,
			RawTokens:        1000,
			CompressedTokens: 225,
			SavedTokens:      775,
		},
	}})

	panel := app.ContextPanel("tab")
	if panel.CompressionEvents != 1 {
		t.Fatalf("compression events = %d, want 1", panel.CompressionEvents)
	}
	if panel.CompressionRawTokens != 1000 || panel.CompressionCompressedTokens != 225 || panel.CompressionSavedTokens != 775 {
		t.Fatalf("compression token metrics = raw:%d compressed:%d saved:%d, want 1000/225/775",
			panel.CompressionRawTokens, panel.CompressionCompressedTokens, panel.CompressionSavedTokens)
	}
	if panel.CompressionRawChars != 4000 || panel.CompressionCompressedChars != 900 || panel.CompressionSavedChars != 3100 {
		t.Fatalf("compression char metrics = raw:%d compressed:%d saved:%d, want 4000/900/3100",
			panel.CompressionRawChars, panel.CompressionCompressedChars, panel.CompressionSavedChars)
	}
}

func TestContextPanelCompressionTelemetryAccumulatesAcrossTurns(t *testing.T) {
	tab := &WorkspaceTab{ID: "tab"}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	sink := &tabEventSink{tabID: "tab", app: app}

	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "tool-1", Name: "bash",
		Compression: &event.Compression{RawTokens: 1000, CompressedTokens: 250, SavedTokens: 750},
	}})
	sink.Emit(event.Event{Kind: event.TurnDone})

	first := app.ContextPanel("tab")
	if first.CompressionEvents != 1 || first.CompressionRawTokens != 1000 || first.CompressionSavedTokens != 750 {
		t.Fatalf("first turn compression = events:%d raw:%d saved:%d, want 1/1000/750",
			first.CompressionEvents, first.CompressionRawTokens, first.CompressionSavedTokens)
	}

	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "tool-2", Name: "bash",
		Compression: &event.Compression{RawTokens: 200, CompressedTokens: 50, SavedTokens: 150},
	}})
	sink.Emit(event.Event{Kind: event.TurnDone})

	second := app.ContextPanel("tab")
	if second.CompressionEvents != 2 || second.CompressionRawTokens != 1200 || second.CompressionCompressedTokens != 300 || second.CompressionSavedTokens != 900 {
		t.Fatalf("second turn compression = events:%d raw:%d compressed:%d saved:%d, want session total 2/1200/300/900",
			second.CompressionEvents, second.CompressionRawTokens, second.CompressionCompressedTokens, second.CompressionSavedTokens)
	}
}

func TestContextPanelUsesLastUsageBreakdownWithTelemetryTotal(t *testing.T) {
	lastUsage := &provider.Usage{
		PromptTokens:     10,
		CompletionTokens: 4,
		TotalTokens:      14,
		CacheHitTokens:   7,
		CacheMissTokens:  3,
		ReasoningTokens:  2,
	}
	ag := agent.New(
		usageProvider{usage: lastUsage},
		tool.NewRegistry(),
		agent.NewSession("system"),
		agent.Options{ContextWindow: 200},
		event.Discard,
	)
	if err := ag.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{
		ID:    "tab",
		Ctrl:  control.New(control.Options{Executor: ag, Sink: event.Discard}),
		Scope: "global",
		Ready: true,
	}
	tab.recordUsage(event.Event{
		Usage: &provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 40,
			TotalTokens:      140,
			CacheHitTokens:   70,
			CacheMissTokens:  30,
			ReasoningTokens:  10,
		},
	})
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}

	panel := app.ContextPanel("tab")
	if panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want telemetry total 140", panel.TotalTokens)
	}
	if panel.PromptTokens != 10 || panel.CompletionTokens != 4 || panel.ReasoningTokens != 2 {
		t.Fatalf("context panel breakdown = prompt:%d completion:%d reasoning:%d, want last usage 10/4/2",
			panel.PromptTokens, panel.CompletionTokens, panel.ReasoningTokens)
	}
	if panel.CacheHitTokens != 7 || panel.CacheMissTokens != 3 {
		t.Fatalf("context panel cache breakdown = hit:%d miss:%d, want last usage 7/3",
			panel.CacheHitTokens, panel.CacheMissTokens)
	}
}
