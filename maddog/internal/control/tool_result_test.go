package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maddog/internal/agent"
	"maddog/internal/agent/testutil"
	"maddog/internal/contextpack"
	"maddog/internal/event"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type toolResultStaticTool struct {
	name     string
	output   string
	readOnly bool
}

func (t toolResultStaticTool) Name() string        { return t.name }
func (t toolResultStaticTool) Description() string { return "static output tool" }
func (t toolResultStaticTool) ReadOnly() bool      { return t.readOnly }
func (t toolResultStaticTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t toolResultStaticTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}

func TestToolResultReturnsRawCompressedOutputAndSessionFallback(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 90; i++ {
		raw.WriteString("INFO heartbeat ready\n")
	}
	raw.WriteString("--- FAIL: TestAddsNumbers (0.01s)\n")
	raw.WriteString("    math/add_test.go:42: expected 4, got 5\n")
	raw.WriteString("FAIL\n")
	raw.WriteString("exit status 1\n")
	rawOutput := raw.String()
	plainOutput := "plain session output"

	reg := tool.NewRegistry()
	reg.Add(toolResultStaticTool{name: "bash", output: rawOutput, readOnly: true})
	reg.Add(toolResultStaticTool{name: "cat", output: plainOutput, readOnly: true})
	compressedArgs := `{"command":"go test ./..."}`
	plainArgs := `{"path":"README.md"}`
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "tool-compressed", Name: "bash", Arguments: compressedArgs},
			{ID: "tool-plain", Name: "cat", Arguments: plainArgs},
		}},
		testutil.Turn{Text: "done"},
	)
	exec := agent.New(prov, reg, agent.NewSession(""), agent.Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 260},
	}, event.Discard)
	if err := exec.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	c := New(Options{Executor: exec})

	compressed := c.ToolResult("tool-compressed")
	if compressed == nil {
		t.Fatal("ToolResult(tool-compressed) = nil")
	}
	if compressed.Args != compressedArgs {
		t.Fatalf("compressed Args = %q, want %q", compressed.Args, compressedArgs)
	}
	if compressed.Output != rawOutput {
		t.Fatalf("compressed Output length = %d, want raw length %d", len(compressed.Output), len(rawOutput))
	}

	plain := c.ToolResult("tool-plain")
	if plain == nil {
		t.Fatal("ToolResult(tool-plain) = nil")
	}
	if plain.Args != plainArgs {
		t.Fatalf("plain Args = %q, want %q", plain.Args, plainArgs)
	}
	if plain.Output != plainOutput {
		t.Fatalf("plain Output = %q, want session output", plain.Output)
	}
}
