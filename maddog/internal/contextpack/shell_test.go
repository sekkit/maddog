package contextpack

import (
	"strings"
	"testing"
)

func TestShellCompressorGoTestFailureExtractsHighSignal(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 60; i++ {
		raw.WriteString("?   \tmaddog/internal/noise\t[no test files]\n")
	}
	raw.WriteString("--- FAIL: TestCheckoutRejectsExpiredToken (0.01s)\n")
	raw.WriteString("    auth/checkout_test.go:42: expected status 200\n")
	raw.WriteString("    auth/checkout_test.go:43: actual status 401\n")
	raw.WriteString("panic: token expired\n")
	raw.WriteString("FAIL\tmaddog/internal/auth\t0.214s\n")
	raw.WriteString("exit status 1\n")

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"go test ./internal/auth -run TestCheckoutRejectsExpiredToken"}`,
		Output:   raw.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 360, RawRef: "raw://tool/go-test"})

	if !got.Route.IsCompression() {
		t.Fatalf("go test output was not compressed: %+v", got)
	}
	if got.Strategy != "go-test-failure" {
		t.Fatalf("strategy = %q, want go-test-failure; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{
		"TestCheckoutRejectsExpiredToken",
		"auth/checkout_test.go:42",
		"expected status 200",
		"actual status 401",
		"panic: token expired",
		"exit status 1",
	} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed go test output missing %q:\n%s", want, got.Content)
		}
	}
	if strings.Count(got.Content, "[no test files]") > 1 {
		t.Fatalf("go test compression kept too much passing-package noise:\n%s", got.Content)
	}
}

func TestShellCompressorTinyBudgetKeepsGoTestSignalBeforeHeader(t *testing.T) {
	raw := strings.Repeat("ok noise\n", 20) +
		"--- FAIL: TestTinyBudget (0.01s)\n" +
		"    tiny_test.go:7: expected true\n" +
		"exit status 1\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"go test ./..."}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 16, MaxBytes: len("--- FAIL: TestTinyBudget (0.01s)")})

	if !strings.Contains(got.Content, "TestTinyBudget") || strings.Contains(got.Content, "summary") {
		t.Fatalf("tiny go test content should keep failure before header:\n%s", got.Content)
	}
}

func TestShellCompressorNPMBuildKeepsFirstFatalError(t *testing.T) {
	raw := strings.Repeat("vite: transforming module\n", 80) +
		"src/App.tsx:12:7 - error TS2322: Type 'number' is not assignable to type 'string'.\n" +
		"12 const title: string = 42\n" +
		"         ~~~~~\n" +
		"npm ERR! code ELIFECYCLE\n" +
		"npm ERR! command failed\n" +
		"npm ERR! exit code 2\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"npm run build"}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 320, RawRef: "raw://tool/npm-build"})

	if !got.Route.IsCompression() {
		t.Fatalf("npm build output was not compressed: %+v", got)
	}
	if got.Strategy != "npm-build-error" {
		t.Fatalf("strategy = %q, want npm-build-error; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{"src/App.tsx:12:7", "TS2322", "Type 'number'", "ELIFECYCLE", "exit code 2"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed npm build output missing %q:\n%s", want, got.Content)
		}
	}
	if strings.Count(got.Content, "vite: transforming module") > 1 {
		t.Fatalf("npm build compression kept too much build noise:\n%s", got.Content)
	}
}

func TestShellCompressorTinyBudgetKeepsNPMErrorBeforeHeader(t *testing.T) {
	raw := strings.Repeat("vite transforming\n", 20) +
		"src/App.tsx:12:7 - error TS2322: Type mismatch\n" +
		"npm ERR! exit code 2\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"npm run build"}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 16, MaxBytes: len("src/App.tsx:12:7 - error")})

	if !strings.Contains(got.Content, "src/App.tsx:12:7") || strings.Contains(got.Content, "summary") {
		t.Fatalf("tiny npm build content should keep error before header:\n%s", got.Content)
	}
}

func TestShellCompressorNPMTestKeepsFailureAndTail(t *testing.T) {
	raw := strings.Repeat(" PASS  src/noise.test.ts\n", 50) +
		" FAIL  src/auth.test.ts\n" +
		"  ● rejects expired token\n" +
		"    expect(received).toEqual(expected)\n" +
		"    Expected: 200\n" +
		"    Received: 401\n" +
		"      at src/auth.test.ts:27:18\n" +
		"Test Suites: 1 failed, 12 passed, 13 total\n" +
		"Tests:       1 failed, 120 passed, 121 total\n" +
		"tail sentinel: npm test complete\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"npm test -- --runInBand"}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 420, RawRef: "raw://tool/npm-test"})

	if !got.Route.IsCompression() {
		t.Fatalf("npm test output was not compressed: %+v", got)
	}
	if got.Strategy != "npm-test-failure" {
		t.Fatalf("strategy = %q, want npm-test-failure; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{"src/auth.test.ts", "rejects expired token", "Expected: 200", "Received: 401", "src/auth.test.ts:27:18", "tail sentinel"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed npm test output missing %q:\n%s", want, got.Content)
		}
	}
}

func TestShellCompressorGitStatusSummarizesAndKeepsTail(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("## main-v2...origin/main-v2\n")
	for i := 1; i <= 20; i++ {
		raw.WriteString(" M pkg/file")
		raw.WriteString(decimal(i))
		raw.WriteString(".go\n")
	}
	raw.WriteString("?? docs/new-plan.md\n")
	raw.WriteString("tail sentinel: status done\n")

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"git status --short --branch"}`,
		Output:   raw.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 360, RawRef: "raw://tool/git-status"})

	if !got.Route.IsCompression() {
		t.Fatalf("git status output was not compressed: %+v", got)
	}
	if got.Strategy != "git-status-summary" {
		t.Fatalf("strategy = %q, want git-status-summary; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{"## main-v2", "modified: 20", "untracked: 1", "pkg/file1.go", "docs/new-plan.md", "tail sentinel"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed git status output missing %q:\n%s", want, got.Content)
		}
	}
}

func TestShellCompressorGitDiffPreservesFileHeadersAndHunkTail(t *testing.T) {
	var raw strings.Builder
	for i := 1; i <= 12; i++ {
		raw.WriteString("diff --git a/pkg/file")
		raw.WriteString(decimal(i))
		raw.WriteString(".go b/pkg/file")
		raw.WriteString(decimal(i))
		raw.WriteString(".go\n")
		raw.WriteString("@@ -1,3 +1,4 @@\n")
		raw.WriteString("+changed line\n")
	}
	raw.WriteString("tail sentinel: diff done\n")

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"git diff"}`,
		Output:   raw.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 420, RawRef: "raw://tool/git-diff"})

	if !got.Route.IsCompression() {
		t.Fatalf("git diff output was not compressed: %+v", got)
	}
	if got.Strategy != "git-diff-summary" {
		t.Fatalf("strategy = %q, want git-diff-summary; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{"12 files changed", "pkg/file1.go", "@@ -1,3 +1,4 @@", "tail sentinel"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed git diff output missing %q:\n%s", want, got.Content)
		}
	}
}

func TestShellCompressorDedupeRepeatedNonAdjacentLogs(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 40; i++ {
		raw.WriteString("GET /health 200\n")
		raw.WriteString("worker tick\n")
	}
	raw.WriteString("ERROR api/server.go:88: database unavailable\n")
	raw.WriteString("tail sentinel: log drained\n")

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"npm run dev"}`,
		Output:   raw.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 260, RawRef: "raw://tool/server-log"})

	if !got.Route.IsCompression() {
		t.Fatalf("server log output was not compressed: %+v", got)
	}
	if got.Strategy != "server-log-dedupe" {
		t.Fatalf("strategy = %q, want server-log-dedupe; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{"GET /health 200 (repeated 40 times)", "worker tick (repeated 40 times)", "api/server.go:88", "tail sentinel"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed server log missing %q:\n%s", want, got.Content)
		}
	}
}

func TestShellCompressorRipgrepAggregatesByFile(t *testing.T) {
	var raw strings.Builder
	for i := 1; i <= 24; i++ {
		raw.WriteString("internal/agent/agent.go:")
		raw.WriteString(decimal(i))
		raw.WriteString(":TODO: follow up\n")
	}
	for i := 1; i <= 12; i++ {
		raw.WriteString("desktop/app.go:")
		raw.WriteString(decimal(i + 100))
		raw.WriteString(":TODO: wire UI\n")
	}

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"rg TODO"}`,
		Output:   raw.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 320, RawRef: "raw://tool/rg"})

	if !got.Route.IsCompression() {
		t.Fatalf("rg output was not compressed: %+v", got)
	}
	if got.Strategy != "rg-file-sampling" {
		t.Fatalf("strategy = %q, want rg-file-sampling; content:\n%s", got.Strategy, got.Content)
	}
	for _, want := range []string{
		"internal/agent/agent.go (24 matches)",
		"desktop/app.go (12 matches)",
		"internal/agent/agent.go:1:TODO",
		"desktop/app.go:101:TODO",
	} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed rg output missing %q:\n%s", want, got.Content)
		}
	}
}

func TestShellCompressorRipgrepParsesColumnsWindowsAndColonMatches(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 12; i++ {
		raw.WriteString(`C:\repo\pkg\file.go:12:404: not found`)
		raw.WriteByte('\n')
		raw.WriteString(`C:\repo\pkg\file.go:13:27:TODO: handle code 500: retry`)
		raw.WriteByte('\n')
		raw.WriteString(`internal/app.ts:7:15:const status = "404: not found"`)
		raw.WriteByte('\n')
		raw.WriteString(`internal/app.ts:8:TODO: keep this`)
		raw.WriteByte('\n')
	}

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"rg --line-number --column TODO 404"}`,
		Output:   raw.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 16, MaxBytes: 420, RawRef: "raw://tool/rg-columns"})

	if !got.Route.IsCompression() {
		t.Fatalf("rg column output was not compressed: %+v", got)
	}
	for _, want := range []string{
		`C:\repo\pkg\file.go (24 matches)`,
		`internal/app.ts (24 matches)`,
		`C:\repo\pkg\file.go:12:404: not found`,
		`C:\repo\pkg\file.go:13:27:TODO`,
		`internal/app.ts:7:15:const status = "404: not found"`,
	} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed rg column output missing %q:\n%s", want, got.Content)
		}
	}
	if strings.Contains(got.Content, `C:\repo\pkg\file.go:12 (`) || strings.Contains(got.Content, "internal/app.ts:7 (") {
		t.Fatalf("rg grouping treated line/column as part of path:\n%s", got.Content)
	}
}

func TestShellCompressorTinyBudgetKeepsRipgrepMatchBeforeHeader(t *testing.T) {
	raw := strings.Repeat("internal/a.go:1:TODO noise\n", 20)

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"rg TODO"}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 16, MaxBytes: len("internal/a.go (20 matches)")})

	if !strings.Contains(got.Content, "internal/a.go") || strings.Contains(got.Content, "summary") {
		t.Fatalf("tiny rg content should keep match before header:\n%s", got.Content)
	}
}
