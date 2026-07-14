package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"maddog/internal/agent/testutil"
	"maddog/internal/contextpack"
	"maddog/internal/event"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type contextpackStaticTool struct {
	name     string
	output   string
	readOnly bool
}

func (t contextpackStaticTool) Name() string        { return t.name }
func (t contextpackStaticTool) Description() string { return "static output tool" }
func (t contextpackStaticTool) ReadOnly() bool      { return t.readOnly }
func (t contextpackStaticTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t contextpackStaticTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}

type panicToolOutputCompressor struct{}

func (panicToolOutputCompressor) Compress(contextpack.ToolOutput, contextpack.Options) contextpack.Result {
	panic("compress boom")
}

type fixedToolOutputCompressor struct {
	result contextpack.Result
}

func (c fixedToolOutputCompressor) Compress(contextpack.ToolOutput, contextpack.Options) contextpack.Result {
	return c.result
}

func TestToolOutputCompressionRejectsModelTokenExpansion(t *testing.T) {
	rawOutput := strings.Repeat("abcd", 250)
	candidate := strings.Repeat("界", 260)

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-token-guard", Name: "bash", Arguments: `{"command":"custom"}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor: fixedToolOutputCompressor{result: contextpack.Result{
			Content:    candidate,
			Compressed: true,
			Lossy:      true,
		}},
	}, event.Discard)

	if err := a.Run(context.Background(), "run command"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	var toolMsg provider.Message
	for _, msg := range reqs[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-token-guard" {
			toolMsg = msg
			break
		}
	}
	if toolMsg.Content != rawOutput {
		t.Fatalf("model-visible output used token-expanding candidate: got %d bytes, want original %d", len(toolMsg.Content), len(rawOutput))
	}
	if _, ok := a.RawToolResult("tool-token-guard"); ok {
		t.Fatal("token-expanding candidate should not retain a raw artifact")
	}
}

func TestToolOutputCompressionRequiresMinimumTokenSaving(t *testing.T) {
	rawOutput := strings.Repeat("a", 400)
	candidate := strings.Repeat("b", 320)

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-small-saving", Name: "bash", Arguments: `{"command":"custom"}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor: fixedToolOutputCompressor{result: contextpack.Result{
			Content:    candidate,
			Compressed: true,
			Lossy:      true,
		}},
	}, event.Discard)

	if err := a.Run(context.Background(), "run command"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolMsg provider.Message
	for _, msg := range prov.Requests()[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-small-saving" {
			toolMsg = msg
			break
		}
	}
	if toolMsg.Content != rawOutput {
		t.Fatalf("model-visible output accepted negligible saving: got %d bytes, want raw %d", len(toolMsg.Content), len(rawOutput))
	}
}

func TestToolOutputCompressionFeedsModelCompressedAndKeepsRaw(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 90; i++ {
		raw.WriteString("INFO heartbeat ready\n")
	}
	raw.WriteString("--- FAIL: TestAddsNumbers (0.01s)\n")
	raw.WriteString("    math/add_test.go:42: expected 4, got 5\n")
	raw.WriteString("FAIL\n")
	raw.WriteString("exit status 1\n")
	rawOutput := raw.String()

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 260},
	}, event.Discard)

	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	if len(reqs) < 2 {
		t.Fatalf("provider requests = %d, want tool round plus final round", len(reqs))
	}
	var toolMsg provider.Message
	for _, msg := range reqs[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-1" {
			toolMsg = msg
			break
		}
	}
	if toolMsg.Content == "" {
		t.Fatalf("second request missing compressed tool message: %+v", reqs[1].Messages)
	}
	if len(toolMsg.Content) >= len(rawOutput) {
		t.Fatalf("tool message was not compressed: got %d raw %d", len(toolMsg.Content), len(rawOutput))
	}
	for _, want := range []string{"TestAddsNumbers", "math/add_test.go:42", "expected 4, got 5", "raw://tool/tool-1"} {
		if !strings.Contains(toolMsg.Content, want) {
			t.Fatalf("compressed tool message missing %q:\n%s", want, toolMsg.Content)
		}
	}
	if strings.Count(toolMsg.Content, "INFO heartbeat ready") > 1 {
		t.Fatalf("repeated log line was not deduped:\n%s", toolMsg.Content)
	}

	gotRaw, ok := a.RawToolResult("tool-1")
	if !ok || gotRaw != rawOutput {
		t.Fatalf("RawToolResult = (%q, %v), want full raw output", gotRaw, ok)
	}
}

func TestCompressedRawToolResultCanBeRetrievedByModelTool(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n"

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-raw-source", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-raw-read", Name: "tool_result", Arguments: `{"id":"tool-raw-source"}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 260},
	}, event.Discard)

	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	if len(reqs) < 3 {
		t.Fatalf("provider requests = %d, want raw retrieval round", len(reqs))
	}
	var rawToolMsg provider.Message
	for _, msg := range reqs[2].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-raw-read" {
			rawToolMsg = msg
			break
		}
	}
	if rawToolMsg.Content != rawOutput {
		t.Fatalf("tool_result output length = %d, want raw length %d\n%s", len(rawToolMsg.Content), len(rawOutput), rawToolMsg.Content)
	}
}

func TestToolOutputCompressionPolicyOffFeedsModelRawAndStoresNoRawRef(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) + "panic: boom\n"

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-off", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{Policy: "off", ThresholdBytes: 1, MaxBytes: 80},
	}, event.Discard)

	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	var toolMsg provider.Message
	for _, msg := range reqs[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-off" {
			toolMsg = msg
			break
		}
	}
	if toolMsg.Content != rawOutput {
		t.Fatalf("policy=off tool message length = %d, want raw length %d", len(toolMsg.Content), len(rawOutput))
	}
	if _, ok := a.RawToolResult("tool-off"); ok {
		t.Fatal("policy=off should not externalize or retain a raw result")
	}
}

func TestRawToolResultPersistsInSessionScopedStoreAcrossResume(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n" +
		"exit status 1\n"
	rawDir := filepath.Join(t.TempDir(), "raw")

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-persist", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	first := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 260},
		RawToolResultDir:      rawDir,
	}, event.Discard)
	if err := first.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snapshot := first.Session().Snapshot()

	resumed := New(prov, reg, NewSession(""), Options{RawToolResultDir: rawDir}, event.Discard)
	resumed.SetSession(&Session{Messages: snapshot})
	if got, ok := resumed.RawToolResult("tool-persist"); !ok || got != rawOutput {
		t.Fatalf("resumed RawToolResult = (%q, %v), want full raw output", got, ok)
	}
}

func TestRawToolResultStoreWriteFailureFallsBackToRawAndWarns(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) + "panic: boom\n"
	rawStorePath := filepath.Join(t.TempDir(), "raw-store-is-file")
	if err := os.WriteFile(rawStorePath, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-store-fail", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	var warnings []string
	var compression *event.Compression
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Level == event.LevelWarn {
			warnings = append(warnings, e.Text)
		}
		if e.Kind == event.ToolResult && e.Tool.ID == "tool-store-fail" {
			compression = e.Tool.Compression
		}
	})
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 120},
		RawToolResultDir:      rawStorePath,
	}, sink)
	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := a.RawToolResult("tool-store-fail"); ok {
		t.Fatal("raw result should be unavailable after store write failure")
	}
	var toolMsg provider.Message
	for _, msg := range prov.Requests()[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-store-fail" {
			toolMsg = msg
			break
		}
	}
	if toolMsg.Content != rawOutput {
		t.Fatalf("store failure model output = %q, want bounded raw output", toolMsg.Content)
	}
	if compression == nil {
		t.Fatal("store failure passthrough metadata = nil")
	}
	if compression.Route != "passthrough" || compression.Quality != "passthrough" || compression.QualityReason != "store_failed" {
		t.Fatalf("store failure decision metadata = %+v", compression)
	}
	if compression.RawRef != "" || compression.Lossy || compression.SavedChars != 0 || compression.SavedTokens != 0 {
		t.Fatalf("store failure claimed addressable or lossy compression: %+v", compression)
	}
	if got := a.ToolCompressions(); len(got) != 0 {
		t.Fatalf("store failure counted as compression: %+v", got)
	}
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "raw tool result store failed") && strings.Contains(warning, "tool-store-fail") &&
			strings.Contains(warning, "using raw/truncated output") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want raw store failure warning", warnings)
	}
}

func TestToolOutputCompressionEmitsMetadataOnToolResult(t *testing.T) {
	rawOutput := strings.Repeat("INFO heartbeat ready\n", 90) +
		"--- FAIL: TestAddsNumbers (0.01s)\n" +
		"    math/add_test.go:42: expected 4, got 5\n" +
		"FAIL\n" +
		"exit status 1\n"

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-meta", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	var got *event.Compression
	var gotOutput string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult && e.Tool.ID == "tool-meta" {
			got = e.Tool.Compression
			gotOutput = e.Tool.Output
		}
	})
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 260},
	}, sink)

	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got == nil {
		t.Fatal("ToolResult compression metadata = nil, want contextpack metadata")
	}
	if got.RawRef != "raw://tool/tool-meta" || got.Strategy == "" || got.Summary == "" {
		t.Fatalf("ToolResult compression identity metadata = %+v", got)
	}
	if got.Route != "profile" || got.Profile != "go-test" || got.Quality != "degraded" {
		t.Fatalf("ToolResult compression route metadata = %+v", got)
	}
	if !got.Lossy || got.OmittedLines <= 0 || got.QualityReason == "" {
		t.Fatalf("ToolResult compression quality metadata = %+v", got)
	}
	if got.UnparsedLines <= 0 || len(got.UnparsedSamples) == 0 {
		t.Fatalf("ToolResult compression unparsed metadata = %+v", got)
	}
	if got.RawChars <= got.CompressedChars || got.SavedChars <= 0 || got.SavedTokens <= 0 {
		t.Fatalf("ToolResult compression delta metadata = %+v, want positive savings", got)
	}
	if got.CompressedChars != utf8.RuneCountInString(gotOutput) {
		t.Fatalf("ToolResult compressed chars = %d, want final output rune count %d for output:\n%s", got.CompressedChars, utf8.RuneCountInString(gotOutput), gotOutput)
	}
	if got.SavedChars != got.RawChars-got.CompressedChars {
		t.Fatalf("ToolResult saved chars = %d, want raw-compressed delta %d", got.SavedChars, got.RawChars-got.CompressedChars)
	}
	if got.CompressedTokens != estimatedTestTokens(gotOutput) || got.SavedTokens != got.RawTokens-got.CompressedTokens {
		t.Fatalf("ToolResult token metrics = %+v, want estimates from final output", got)
	}
}

func TestToolOutputCompressionMetricsCompareBoundedAlternatives(t *testing.T) {
	rawOutput := strings.Repeat("raw output line that exceeds the model boundary\n", 2000)
	candidate := "bounded summary"

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-bounded-metrics", Name: "bash", Arguments: `{"command":"custom"}`}}},
		testutil.Turn{Text: "done"},
	)
	var got *event.Compression
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult && e.Tool.ID == "tool-bounded-metrics" {
			got = e.Tool.Compression
		}
	})
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor: fixedToolOutputCompressor{result: contextpack.Result{
			Content:    candidate,
			Compressed: true,
			Route:      contextpack.RouteGeneric,
			Profile:    "generic",
			Quality:    contextpack.ParseQualityDegraded,
			Lossy:      true,
		}},
	}, sink)

	if err := a.Run(context.Background(), "run command"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got == nil {
		t.Fatal("ToolResult compression metadata = nil")
	}
	rawVisible, _ := truncateToolOutput(rawOutput)
	if got.RawChars != utf8.RuneCountInString(rawVisible) || got.RawTokens != estimatedTestTokens(rawVisible) {
		t.Fatalf("bounded raw metrics = chars:%d tokens:%d, want chars:%d tokens:%d", got.RawChars, got.RawTokens, utf8.RuneCountInString(rawVisible), estimatedTestTokens(rawVisible))
	}
}

func estimatedTestTokens(s string) int {
	if s == "" {
		return 0
	}
	ascii := 0
	cjk := 0
	other := 0
	for _, r := range s {
		switch {
		case r <= 0x7f:
			ascii++
		case (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf) || (r >= 0x3040 && r <= 0x30ff) || (r >= 0xac00 && r <= 0xd7af):
			cjk++
		default:
			other++
		}
	}
	tokens := (ascii + 3) / 4
	tokens += cjk
	tokens += (other + 1) / 2
	if tokens == 0 {
		return 1
	}
	return tokens
}

func TestToolOutputCompressorPanicFallsBackToTruncatedRawAndWarns(t *testing.T) {
	line := "middle filler line\n"
	rawOutput := "HEAD-SENTINEL\n" + strings.Repeat(line, maxToolOutputBytes/len(line)+100) + "TAIL-SENTINEL\n"

	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-panic", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{Text: "done"},
	)
	var warnings []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Level == event.LevelWarn {
			warnings = append(warnings, e.Text)
		}
	})
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor: panicToolOutputCompressor{},
	}, sink)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Run panicked: %v", r)
		}
	}()
	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	if len(reqs) < 2 {
		t.Fatalf("provider requests = %d, want tool round plus final round", len(reqs))
	}
	var toolMsg provider.Message
	for _, msg := range reqs[1].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "tool-panic" {
			toolMsg = msg
			break
		}
	}
	if toolMsg.Content == "" {
		t.Fatalf("second request missing fallback tool message: %+v", reqs[1].Messages)
	}
	for _, want := range []string{"HEAD-SENTINEL", "TAIL-SENTINEL", "[truncated"} {
		if !strings.Contains(toolMsg.Content, want) {
			t.Fatalf("fallback tool message missing %q:\n%s", want, toolMsg.Content)
		}
	}
	for _, bad := range []string{"raw://tool/tool-panic", "[compressed tool output"} {
		if strings.Contains(toolMsg.Content, bad) {
			t.Fatalf("fallback tool message unexpectedly contained %q:\n%s", bad, toolMsg.Content)
		}
	}
	if len(toolMsg.Content) >= len(rawOutput) {
		t.Fatalf("fallback tool message length = %d, want truncated below raw length %d", len(toolMsg.Content), len(rawOutput))
	}
	if gotRaw, ok := a.RawToolResult("tool-panic"); ok {
		t.Fatalf("RawToolResult stored after compressor panic: %q", gotRaw)
	}
	foundWarning := false
	for _, warning := range warnings {
		if strings.Contains(warning, "tool output compression failed") && strings.Contains(warning, "compress boom") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("warning notices = %v, want compressor failure warning", warnings)
	}
}
