package contextpack

import (
	"strconv"
	"strings"
	"testing"
)

func TestCompressCompoundShellCommandAvoidsSingleCommandProfile(t *testing.T) {
	raw := strings.Repeat(" M pkg/file.go\n", 40) +
		"--- FAIL: TestCompoundFailure (0.01s)\n" +
		"    pkg/file_test.go:12: expected true\n" +
		"FAIL\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"git status --short && go test ./..."}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 64, MaxBytes: 320, RawRef: "raw://tool/compound"})

	if !got.Route.IsCompression() {
		t.Fatalf("compound output should still receive conservative generic compression: %+v", got)
	}
	if got.Strategy == "git-status-summary" || got.Strategy == "go-test-failure" {
		t.Fatalf("compound command used single-command strategy %q:\n%s", got.Strategy, got.Content)
	}
	if got.Route != RouteGeneric || got.Profile != "generic" {
		t.Fatalf("compound command route/profile = %q/%q, want generic/generic", got.Route, got.Profile)
	}
	if !strings.Contains(got.Content, "TestCompoundFailure") {
		t.Fatalf("generic compression lost failure signal:\n%s", got.Content)
	}
}

func TestCompressQuotedCommandTextAvoidsProfile(t *testing.T) {
	raw := strings.Repeat("git status is documentation text\n", 50) + "tail sentinel\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"echo \"git status\""}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 64, MaxBytes: 180, RawRef: "raw://tool/quoted"})

	if !got.Route.IsCompression() {
		t.Fatalf("quoted command output should still receive generic compression: %+v", got)
	}
	if got.Strategy == "git-status-summary" {
		t.Fatalf("quoted argument text incorrectly triggered git status profile:\n%s", got.Content)
	}
	if got.Route != RouteGeneric || got.Profile != "generic" {
		t.Fatalf("quoted command route/profile = %q/%q, want generic/generic", got.Route, got.Profile)
	}
}

func TestCompressExecutableIdentityUsesExecutionGOOS(t *testing.T) {
	raw := "## main...origin/main\n" + strings.Repeat(" M pkg/file.go\n", 40)
	tests := []struct {
		name      string
		goos      string
		command   string
		wantRoute Route
	}{
		{name: "linux preserves executable case", goos: "linux", command: "Git status --short", wantRoute: RouteGeneric},
		{name: "linux preserves exe suffix", goos: "linux", command: "git.exe status --short", wantRoute: RouteGeneric},
		{name: "windows normalizes executable", goos: "windows", command: "Git.EXE status --short", wantRoute: RouteProfile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compress(ToolOutput{
				ToolName: "bash",
				Shell:    "bash",
				GOOS:     tt.goos,
				Args:     `{"command":` + strconv.Quote(tt.command) + `}`,
				Output:   raw,
				ReadOnly: true,
			}, Options{ThresholdBytes: 64, MaxBytes: 240, RawRef: "raw://tool/executable-semantics"})

			if got.Route != tt.wantRoute {
				t.Fatalf("route = %q, want %q; strategy=%q\n%s", got.Route, tt.wantRoute, got.Strategy, got.Content)
			}
		})
	}
}

func TestCompressCommandLexingUsesExecutionShell(t *testing.T) {
	raw := "## main...origin/main\n" + strings.Repeat(" M pkg/file.go\n", 40)
	tests := []struct {
		name      string
		toolName  string
		shell     string
		command   string
		wantRoute Route
	}{
		{
			name:      "powershell backslash does not escape redirect",
			toolName:  "bash",
			shell:     "powershell",
			command:   `git status --short \> status.txt`,
			wantRoute: RouteGeneric,
		},
		{
			name:      "bash backslash escapes redirect",
			toolName:  "powershell",
			shell:     "bash",
			command:   `git status --short \> status.txt`,
			wantRoute: RouteProfile,
		},
		{
			name:      "powershell backtick escapes redirect",
			toolName:  "bash",
			shell:     "pwsh",
			command:   "git status --short `> status.txt",
			wantRoute: RouteProfile,
		},
		{
			name:      "unknown receipt overrides shell-like tool name",
			toolName:  "bash",
			shell:     "unknown-shell",
			command:   "git status --short",
			wantRoute: RouteGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compress(ToolOutput{
				ToolName: tt.toolName,
				Shell:    tt.shell,
				GOOS:     "windows",
				Args:     `{"command":` + strconv.Quote(tt.command) + `}`,
				Output:   raw,
				ReadOnly: true,
			}, Options{ThresholdBytes: 64, MaxBytes: 240, RawRef: "raw://tool/shell-semantics"})

			if got.Route != tt.wantRoute {
				t.Fatalf("route = %q, want %q; strategy=%q\n%s", got.Route, tt.wantRoute, got.Strategy, got.Content)
			}
		})
	}
}

func TestCompressEnvironmentAssignmentUsesExecutionShell(t *testing.T) {
	raw := "## main...origin/main\n" + strings.Repeat(" M pkg/file.go\n", 40)
	tests := []struct {
		name      string
		shell     string
		wantRoute Route
	}{
		{name: "bash assignment prefix", shell: "bash", wantRoute: RouteProfile},
		{name: "powershell assignment is executable text", shell: "powershell", wantRoute: RouteGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compress(ToolOutput{
				ToolName: "bash",
				Shell:    tt.shell,
				GOOS:     "windows",
				Args:     `{"command":"MADDOG_MODE=test git status --short"}`,
				Output:   raw,
				ReadOnly: true,
			}, Options{ThresholdBytes: 64, MaxBytes: 240, RawRef: "raw://tool/assignment-semantics"})

			if got.Route != tt.wantRoute {
				t.Fatalf("route = %q, want %q; strategy=%q\n%s", got.Route, tt.wantRoute, got.Strategy, got.Content)
			}
		})
	}
}

func TestCompressProfilesRespectInvocationShape(t *testing.T) {
	gitRaw := "## main...origin/main\n" + strings.Repeat(" M pkg/file.go\n", 40)
	rgRaw := strings.Repeat(`{"type":"match","data":{"path":"internal/app.go"}}`+"\n", 40)

	tests := []struct {
		name         string
		command      string
		raw          string
		wantStrategy string
		wantRoute    Route
	}{
		{name: "git global directory option", command: "git -C repo status --short --branch", raw: gitRaw, wantStrategy: "git-status-summary", wantRoute: RouteProfile},
		{name: "redirected git status", command: "git status --short > status.txt", raw: gitRaw, wantRoute: RouteGeneric},
		{name: "ripgrep json shape", command: "rg --json TODO", raw: rgRaw, wantRoute: RouteGeneric},
		{name: "git long status", command: "git status", raw: strings.Repeat("Changes not staged for commit:\n  modified: pkg/file.go\n", 20), wantRoute: RouteGeneric},
		{name: "git nul status", command: "git status --short -z", raw: strings.Repeat(" M pkg/file.go\x00", 40), wantRoute: RouteGeneric},
		{name: "git stat diff", command: "git diff --stat", raw: strings.Repeat(" pkg/file.go | 2 +-\n", 40), wantRoute: RouteGeneric},
		{name: "case-sensitive package script", command: "npm run BUILD", raw: strings.Repeat("error: custom BUILD output\n", 40), wantRoute: RouteGeneric},
		{name: "empty quoted argument", command: `git "" status --short`, raw: gitRaw, wantRoute: RouteGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compress(ToolOutput{
				ToolName: "bash",
				Args:     `{"command":` + strconv.Quote(tt.command) + `}`,
				Output:   tt.raw,
				ReadOnly: true,
			}, Options{ThresholdBytes: 64, MaxBytes: 240, RawRef: "raw://tool/shape"})
			if !got.Route.IsCompression() {
				t.Fatalf("output should remain eligible for compression: %+v", got)
			}
			if tt.wantStrategy != "" && got.Strategy != tt.wantStrategy {
				t.Fatalf("strategy = %q, want %q\n%s", got.Strategy, tt.wantStrategy, got.Content)
			}
			if got.Route != tt.wantRoute {
				t.Fatalf("route = %q, want %q; strategy=%q\n%s", got.Route, tt.wantRoute, got.Strategy, got.Content)
			}
		})
	}
}

func TestCompressionResultReportsProfileQuality(t *testing.T) {
	raw := strings.Repeat("?   maddog/internal/noise [no test files]\n", 40) +
		"unparsed runner diagnostic\n" +
		"--- FAIL: TestProfileQuality (0.01s)\n" +
		"    quality_test.go:17: expected exact metadata\n" +
		"FAIL\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"go test ./..."}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 64, MaxBytes: 260, RawRef: "raw://tool/quality"})

	if got.Route != RouteProfile || got.Profile != "go-test" {
		t.Fatalf("route/profile = %q/%q, want %q/go-test: %+v", got.Route, got.Profile, RouteProfile, got)
	}
	if got.Quality != ParseQualityDegraded || got.QualityReason == "" {
		t.Fatalf("quality = %q reason=%q, want explicit degraded reason", got.Quality, got.QualityReason)
	}
	if !got.Route.IsCompression() || got.OmittedLines <= 0 {
		t.Fatalf("loss metadata = lossy:%v omitted:%d, want omitted raw lines", got.Route.IsCompression(), got.OmittedLines)
	}
	if got.UnparsedLines <= 0 || !containsSample(got.UnparsedSamples, "unparsed runner diagnostic") {
		t.Fatalf("unparsed metadata = lines:%d samples:%q, want unknown diagnostic", got.UnparsedLines, got.UnparsedSamples)
	}
}

func TestProfileDegradationPreservesUnknownSignalAndSamples(t *testing.T) {
	raw := strings.Repeat("internal/app.go:12:TODO: follow up\n", 30) +
		"error: permission denied while walking vendor\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"rg TODO"}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 64, MaxBytes: 260, RawRef: "raw://tool/rg-error"})

	if got.Route != RouteProfile || got.Profile != "rg" || got.Quality != ParseQualityDegraded {
		t.Fatalf("profile quality = route:%q profile:%q quality:%q", got.Route, got.Profile, got.Quality)
	}
	if !strings.Contains(got.Content, "permission denied") {
		t.Fatalf("degraded profile dropped unknown error signal:\n%s", got.Content)
	}
	if got.UnparsedLines != 1 || !containsSample(got.UnparsedSamples, "permission denied") {
		t.Fatalf("unparsed metadata = lines:%d samples:%q, want error sample", got.UnparsedLines, got.UnparsedSamples)
	}
}

func TestUnrecognizedProfileOutputFallsBackToGeneric(t *testing.T) {
	raw := strings.Repeat("custom runner progress\n", 50) + "tail sentinel\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"go test ./..."}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 64, MaxBytes: 220, RawRef: "raw://tool/custom-runner"})

	if got.Route != RouteGeneric || got.Profile != "generic" || got.Strategy == "go-test-failure" {
		t.Fatalf("unrecognized go output route/profile/strategy = %q/%q/%q", got.Route, got.Profile, got.Strategy)
	}
}

func containsSample(samples []string, needle string) bool {
	for _, sample := range samples {
		if strings.Contains(sample, needle) {
			return true
		}
	}
	return false
}
