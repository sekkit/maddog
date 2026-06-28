package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ctxcompress "reasonix/internal/context"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type longOutputTool struct {
	output string
}

func (t longOutputTool) Name() string        { return "long_output" }
func (t longOutputTool) Description() string { return "long output" }
func (t longOutputTool) ReadOnly() bool      { return true }
func (t longOutputTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t longOutputTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}

type captureProvider struct {
	requests []provider.Request
	turn     int
}

func (p *captureProvider) Name() string { return "capture" }
func (p *captureProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 4)
	switch p.turn {
	case 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-long", Name: "long_output", Arguments: `{}`}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}
	p.turn++
	close(ch)
	return ch, nil
}

func TestToolOutputCompressorFeedsCompressedResultToModelAndRawToEvent(t *testing.T) {
	raw := "HEAD: start\n" + strings.Repeat("noise\n", 80) + "ERROR: line 12 failed\n" + strings.Repeat("noise\n", 80) + "TAIL: done"
	prov := &captureProvider{}
	reg := tool.NewRegistry()
	reg.Add(longOutputTool{output: raw})
	sink := &recordSink{}
	a := New(prov, reg, NewSession("sys"), Options{
		ToolOutputCompressor: ctxcompress.NewDeterministicCompressor(ctxcompress.CompressOptions{
			ThresholdBytes: 80,
			HeadBytes:      32,
			TailBytes:      32,
			MaxErrorLines:  4,
		}),
	}, sink)

	if err := a.Run(context.Background(), "run long tool"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) < 2 {
		t.Fatalf("provider requests = %d, want at least 2", len(prov.requests))
	}
	var toolMsg string
	for _, msg := range prov.requests[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "call-long" {
			toolMsg = msg.Content
		}
	}
	if !strings.Contains(toolMsg, "[compressed tool output]") || !strings.Contains(toolMsg, "tool://call-long/raw") {
		t.Fatalf("model-facing tool message was not compressed:\n%s", toolMsg)
	}
	if strings.Contains(toolMsg, strings.Repeat("noise\n", 20)) {
		t.Fatalf("model-facing tool message kept raw noise:\n%s", toolMsg)
	}
	events := sink.kinds(event.ToolResult)
	if len(events) == 0 {
		t.Fatalf("missing ToolResult event")
	}
	got := events[0].Tool
	if got.Output != raw {
		t.Fatalf("ToolResult output should stay raw for UI; len got=%d want=%d", len(got.Output), len(raw))
	}
	if got.Compression == nil || !got.Compression.Compressed || got.Compression.RawRef != "tool://call-long/raw" {
		t.Fatalf("ToolResult compression = %+v", got.Compression)
	}
	metrics := a.CompressionMetrics()
	if metrics.CompressedItems != 1 || metrics.SavedBytes <= 0 {
		t.Fatalf("CompressionMetrics = %+v", metrics)
	}
}
