package control

import (
	"context"
	"encoding/json"
	"path/filepath"
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

func TestToolResultMarksMissingRawOutputUnavailable(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n" +
		"exit status 1\n"

	reg := tool.NewRegistry()
	reg.Add(toolResultStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-missing-raw", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	exec := agent.New(prov, reg, agent.NewSession(""), agent.Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 220},
	}, event.Discard)
	if err := exec.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snapshot := exec.Session().Snapshot()
	resumed := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)
	resumed.SetSession(&agent.Session{Messages: snapshot})
	c := New(Options{Executor: resumed})

	got := c.ToolResult("tool-missing-raw")
	if got == nil {
		t.Fatal("ToolResult(tool-missing-raw) = nil")
	}
	if !got.RawUnavailable {
		t.Fatalf("RawUnavailable = false, want true for compressed message without raw backing")
	}
	if got.Output == "" || len(got.Output) >= len(rawOutput) {
		t.Fatalf("fallback output should be compressed content, got length %d raw %d", len(got.Output), len(rawOutput))
	}
}

func TestToolResultLoadsSessionScopedRawOutputAfterResume(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n" +
		"exit status 1\n"
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")

	reg := tool.NewRegistry()
	reg.Add(toolResultStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-session-raw", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	exec := agent.New(prov, reg, agent.NewSession(""), agent.Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 220},
	}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir})
	c.SetSessionPath(sessionPath)
	if err := exec.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snapshot := exec.Session().Snapshot()
	c.Close()

	resumed := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)
	resumed.SetSession(&agent.Session{Messages: snapshot})
	resumedController := New(Options{Executor: resumed, SessionDir: dir})
	resumedController.SetSessionPath(sessionPath)
	got := resumedController.ToolResult("tool-session-raw")
	if got == nil {
		t.Fatal("ToolResult(tool-session-raw) = nil")
	}
	if got.RawUnavailable {
		t.Fatal("RawUnavailable = true, want persisted raw output to be available")
	}
	if got.Output != rawOutput {
		t.Fatalf("ToolResult output length = %d, want raw length %d", len(got.Output), len(rawOutput))
	}
}

func TestResumeBindsSessionScopedRawOutputStore(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n" +
		"exit status 1\n"
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")

	reg := tool.NewRegistry()
	reg.Add(toolResultStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-resume-raw", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	exec := agent.New(prov, reg, agent.NewSession(""), agent.Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 220},
	}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir})
	c.SetSessionPath(sessionPath)
	if err := exec.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snapshot := exec.Session().Snapshot()
	c.Close()

	resumed := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)
	resumedController := New(Options{Executor: resumed, SessionDir: dir})
	if err := resumedController.Resume(&agent.Session{Messages: snapshot}, sessionPath); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	got := resumedController.ToolResult("tool-resume-raw")
	if got == nil {
		t.Fatal("ToolResult(tool-resume-raw) = nil")
	}
	if got.RawUnavailable {
		t.Fatal("RawUnavailable = true, want Resume to bind persisted raw output store")
	}
	if got.Output != rawOutput {
		t.Fatalf("ToolResult output length = %d, want raw length %d", len(got.Output), len(rawOutput))
	}
}

func TestControllerNewBindsInitialSessionPathRawStore(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n" +
		"exit status 1\n"
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")

	reg := tool.NewRegistry()
	reg.Add(toolResultStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-initial-path", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	exec := agent.New(prov, reg, agent.NewSession(""), agent.Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 220},
	}, event.Discard)
	_ = New(Options{Executor: exec, SessionDir: dir, SessionPath: sessionPath})
	if err := exec.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snapshot := exec.Session().Snapshot()

	resumed := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)
	resumed.SetSession(&agent.Session{Messages: snapshot})
	_ = New(Options{Executor: resumed, SessionDir: dir, SessionPath: sessionPath})
	if got, ok := resumed.RawToolResult("tool-initial-path"); !ok || got != rawOutput {
		t.Fatalf("resumed RawToolResult with initial SessionPath = (%q, %v), want persisted raw output", got, ok)
	}
}
