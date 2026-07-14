package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestRawToolResultToolPaginatesLargeResultsWithoutLoss(t *testing.T) {
	raw := "HEAD-SENTINEL\n" + strings.Repeat("payload with unicode 你好\n", 2_000) + "TAIL-SENTINEL\n"
	a := &Agent{rawToolResults: map[string]string{"tool-1": raw}}
	tool := rawToolResultTool{agent: a}

	var restored strings.Builder
	offset := 0
	for {
		args := fmt.Sprintf(`{"id":"raw://tool/tool-1","offset":%d,"limit":1024}`, offset)
		out, err := tool.Execute(context.Background(), []byte(args))
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		if len(out) > maxToolOutputBytes {
			t.Fatalf("model-visible page is %d bytes, exceeds safe tool output bound %d", len(out), maxToolOutputBytes)
		}
		header, page, ok := strings.Cut(out, "\n\n")
		if !ok {
			t.Fatalf("page at offset %d has no pagination header: %q", offset, out)
		}
		restored.WriteString(page)
		next, more, err := rawToolResultNextOffset(header)
		if err != nil {
			t.Fatalf("page header %q: %v", header, err)
		}
		if !more {
			break
		}
		if next <= offset {
			t.Fatalf("next offset %d did not advance from %d", next, offset)
		}
		offset = next
	}
	if got := restored.String(); got != raw {
		t.Fatalf("reassembled raw output differs: got %d bytes, want %d", len(got), len(raw))
	}
}

func TestRawToolResultToolKeepsSmallResultsVerbatim(t *testing.T) {
	a := &Agent{rawToolResults: map[string]string{"tool-1": "one\ntwo\n"}}
	out, err := (rawToolResultTool{agent: a}).Execute(context.Background(), []byte(`{"id":"tool-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "one\ntwo\n" {
		t.Fatalf("small raw result = %q, want original text", out)
	}
}

func TestRawToolResultToolKeepsLegacyLargeResultsVerbatim(t *testing.T) {
	raw := "HEAD-SENTINEL\n" + strings.Repeat("payload\n", defaultRawToolResultPageBytes) + "TAIL-SENTINEL\n"
	a := &Agent{rawToolResults: map[string]string{"tool-1": raw}}
	out, err := (rawToolResultTool{agent: a}).Execute(context.Background(), []byte(`{"id":"tool-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != raw {
		t.Fatalf("legacy id-only result = %d bytes, want original %d bytes", len(out), len(raw))
	}
}

func TestRawToolResultToolRejectsInvalidPages(t *testing.T) {
	a := &Agent{rawToolResults: map[string]string{"tool-1": "a你b"}}
	tool := rawToolResultTool{agent: a}
	for _, args := range []string{
		`{"id":"tool-1","offset":-1}`,
		`{"id":"tool-1","offset":2}`,
		`{"id":"tool-1","limit":1}`,
		`{"id":"tool-1","limit":2}`,
		`{"id":"tool-1","limit":3}`,
		`{"id":"tool-1","limit":16385}`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Errorf("Execute(%s) succeeded, want invalid page error", args)
		}
	}
}

func rawToolResultNextOffset(header string) (int, bool, error) {
	if strings.Contains(header, "end_of_result") {
		return 0, false, nil
	}
	const prefix = "next_offset="
	index := strings.Index(header, prefix)
	if index < 0 {
		return 0, false, fmt.Errorf("missing %q", prefix)
	}
	value := strings.TrimSuffix(header[index+len(prefix):], "]")
	next, err := strconv.Atoi(value)
	if err != nil {
		return 0, false, err
	}
	return next, true, nil
}
