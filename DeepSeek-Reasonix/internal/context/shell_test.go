package context

import (
	"strings"
	"testing"
)

func TestSummarizeShellOutputPreservesFailuresAndFileLines(t *testing.T) {
	output := strings.Join([]string{
		"ok   reasonix/internal/context 0.01s",
		"--- FAIL: TestThing (0.00s)",
		"    shell_test.go:42: expected summary",
		"FAIL",
		"panic: boom",
	}, "\n")

	got := SummarizeShellOutput(ShellOutput{Command: "go test ./...", Output: output, MaxLines: 6})

	for _, want := range []string{"go test ./...", "--- FAIL: TestThing", "shell_test.go:42", "panic: boom"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestSummarizeShellOutputDedupesRepeatedLogs(t *testing.T) {
	output := strings.Join([]string{
		"server: retrying connection",
		"server: retrying connection",
		"server: retrying connection",
		"ERROR app/routes.ts:17 failed to compile",
		"server: retrying connection",
	}, "\n")

	got := SummarizeShellOutput(ShellOutput{Command: "npm run build", Output: output, MaxLines: 8})

	if strings.Count(got, "server: retrying connection") != 1 {
		t.Fatalf("summary should dedupe repeated logs:\n%s", got)
	}
	if !strings.Contains(got, "repeated 4x") || !strings.Contains(got, "app/routes.ts:17") {
		t.Fatalf("summary missing repeat count or file line:\n%s", got)
	}
}

func TestSummarizeShellOutputEmptyFallback(t *testing.T) {
	got := SummarizeShellOutput(ShellOutput{Command: "rg missing", Output: "", MaxLines: 4})
	if !strings.Contains(got, "no output") || !strings.Contains(got, "rg missing") {
		t.Fatalf("empty summary = %q", got)
	}
}
