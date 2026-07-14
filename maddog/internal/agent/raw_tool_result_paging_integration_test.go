package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maddog/internal/agent/testutil"
	"maddog/internal/contextpack"
	"maddog/internal/event"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

func TestRawToolResultExplicitPageFeedsModelWithoutTruncation(t *testing.T) {
	rawOutput := "HEAD-SENTINEL\n" + strings.Repeat("payload with unicode 你好\n", 2_000) + "TAIL-SENTINEL\n"
	a, prov := runRawToolResultRound(t, rawOutput, `{"id":"tool-source","offset":0,"limit":16384}`)

	got := toolResultMessage(t, prov.Requests(), "tool-raw-read")
	want, err := (rawToolResultTool{agent: a}).Execute(context.Background(), json.RawMessage(`{"id":"tool-source","offset":0,"limit":16384}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != want {
		t.Fatalf("model saw a changed raw page:\n--- got ---\n%s\n--- want ---\n%s", got.Content, want)
	}
	if len(got.Content) > maxToolOutputBytes {
		t.Fatalf("paged raw result is %d bytes, exceeds %d-byte model safety bound", len(got.Content), maxToolOutputBytes)
	}
	_, page, ok := strings.Cut(got.Content, "\n\n")
	if !ok {
		t.Fatalf("paged raw result has no header: %q", got.Content)
	}
	if len(page) > maxRawToolResultPageBytes {
		t.Fatalf("raw page is %d bytes, exceeds requested limit %d", len(page), maxRawToolResultPageBytes)
	}
	for _, unexpected := range []string{"[truncated", "TAIL-SENTINEL"} {
		if strings.Contains(got.Content, unexpected) {
			t.Fatalf("explicit first page unexpectedly contains %q:\n%s", unexpected, got.Content)
		}
	}
	if !strings.Contains(got.Content, "next_offset=") {
		t.Fatalf("explicit page is missing a continuation offset:\n%s", got.Content)
	}
}

func TestRawToolResultLegacyReadKeepsHeadAndTailDiagnostics(t *testing.T) {
	rawOutput := "HEAD-SENTINEL\n" + strings.Repeat("middle payload\n", maxToolOutputBytes*2) + "TAIL-SENTINEL\n"
	_, prov := runRawToolResultRound(t, rawOutput, `{"id":"tool-source"}`)

	got := toolResultMessage(t, prov.Requests(), "tool-raw-read")
	if got.Content == rawOutput {
		t.Fatal("legacy large raw result bypassed the model safety truncation")
	}
	for _, want := range []string{"HEAD-SENTINEL", "TAIL-SENTINEL", "[truncated"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("legacy raw result missing %q:\n%s", want, got.Content)
		}
	}
}

func runRawToolResultRound(t *testing.T, rawOutput, rawReadArgs string) (*Agent, *testutil.MockProvider) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(contextpackStaticTool{name: "bash", output: rawOutput, readOnly: true})
	prov := testutil.NewMock("mock",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-source", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "tool-raw-read", Name: "tool_result", Arguments: rawReadArgs}}},
		testutil.Turn{Text: "done"},
	)
	a := New(prov, reg, NewSession(""), Options{
		ToolOutputCompressor:  contextpack.DefaultCompressor{},
		ToolOutputCompression: contextpack.Options{ThresholdBytes: 128, MaxBytes: 260},
	}, event.Discard)
	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return a, prov
}

func toolResultMessage(t *testing.T, requests []provider.Request, callID string) provider.Message {
	t.Helper()
	if len(requests) < 3 {
		t.Fatalf("provider requests = %d, want at least 3", len(requests))
	}
	for _, msg := range requests[2].Messages {
		if msg.Role == provider.RoleTool && msg.ToolCallID == callID {
			return msg
		}
	}
	t.Fatalf("request has no tool result for %q: %+v", callID, requests[2].Messages)
	return provider.Message{}
}
