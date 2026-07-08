package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/codegraph"
	"maddog/internal/config"
)

func TestRedactHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)        // os.UserHomeDir on unix
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on windows
	sep := string(os.PathSeparator)

	if got := redactHome(home); got != "~" {
		t.Fatalf("home itself: got %q, want ~", got)
	}
	under := filepath.Join(home, "projects", "x")
	if got, want := redactHome(under), "~"+sep+"projects"+sep+"x"; got != want {
		t.Fatalf("under home: got %q, want %q", got, want)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere") // sibling temp, not under home
	if got := redactHome(outside); got != outside {
		t.Fatalf("outside home must be unchanged: got %q", got)
	}
	if got := redactHome(""); got != "" {
		t.Fatalf("empty must stay empty: got %q", got)
	}
}

func TestCollectReportRedactsSecrets(t *testing.T) {
	t.Setenv("MADDOG_TEST_SECRET", "sk-live-secret")

	cfg := config.Default()
	cfg.DefaultModel = "custom"
	cfg.Providers = []config.ProviderEntry{{
		Name:      "custom",
		Kind:      "openai",
		BaseURL:   "https://api.example.com/v1?token=secret-query",
		Model:     "model-a",
		APIKeyEnv: "MADDOG_TEST_SECRET",
	}}
	cfg.Plugins = []config.PluginEntry{{
		Name:    "remote",
		Type:    "http",
		URL:     "https://mcp.example.com/path?api_key=secret-query",
		Headers: map[string]string{"Authorization": "Bearer sk-live-secret"},
	}}
	cfg.Network = config.NetworkConfig{
		ProxyMode: "custom",
		Proxy: config.NetworkProxyConfig{
			Type:     "socks5",
			Server:   "proxy.example.com",
			Port:     1080,
			Username: "proxy-user",
			Password: "proxy-secret",
		},
	}

	report := Collect(Options{Version: "test-version", Config: cfg})
	text := RenderText(report)
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	combined := text + "\n" + string(raw)

	for _, secret := range []string{"sk-live-secret", "secret-query", "Authorization", "proxy-secret"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("doctor report leaked %q:\n%s", secret, combined)
		}
	}
	if !strings.Contains(combined, "api.example.com") || !strings.Contains(combined, "mcp.example.com") {
		t.Fatalf("doctor report should keep useful host diagnostics:\n%s", combined)
	}
}

func TestCollectReportDoesNotRequireAPIKey(t *testing.T) {
	t.Setenv("MADDOG_HOME", filepath.Join(t.TempDir(), "maddog"))
	t.Setenv("DEEPSEEK_API_KEY", "")

	cfg := config.Default()
	report := Collect(Options{Version: "1.2.3", Config: cfg})
	text := RenderText(report)

	if report.Version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", report.Version)
	}
	if len(report.Providers) == 0 {
		t.Fatal("expected built-in providers in report")
	}
	if report.Providers[0].KeyPresent {
		t.Fatal("provider key should be reported missing when env is empty")
	}
	if !strings.Contains(text, "maddog 1.2.3 doctor") {
		t.Fatalf("text report missing header:\n%s", text)
	}
	if !strings.Contains(text, "missing") {
		t.Fatalf("text report should mention missing key state:\n%s", text)
	}
}

func TestRenderTextSurfacesWarningsUpTop(t *testing.T) {
	text := RenderText(Report{Warnings: []string{"config maddog.toml: parse boom"}})
	w := strings.Index(text, "parse boom")
	if w < 0 {
		t.Fatalf("warning missing from report:\n%s", text)
	}
	if p := strings.Index(text, "\nproviders\n"); p >= 0 && w > p {
		t.Fatalf("warning should appear before the providers section, not buried below:\n%s", text)
	}
}

func TestRenderTextFlagsInactiveSandbox(t *testing.T) {
	inactive := RenderText(Report{Sandbox: SandboxReport{Bash: "enforce", Available: false}})
	if !strings.Contains(inactive, "inactive") {
		t.Fatalf("enforce without an OS sandbox should be flagged inactive:\n%s", inactive)
	}

	active := RenderText(Report{Sandbox: SandboxReport{Bash: "enforce", Available: true}})
	if strings.Contains(active, "inactive") {
		t.Fatalf("enforce with an OS sandbox should not be flagged inactive:\n%s", active)
	}
}

func TestCollectReportIncludesLatestCodeIntelligenceBenchmark(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("APPDATA", cacheRoot)
	t.Setenv("XDG_CONFIG_HOME", cacheRoot)

	report := codegraph.BenchmarkReport{
		Backends: []codegraph.BenchmarkBackendReport{{
			ID:     "mock",
			Name:   "MockGraph",
			Health: codegraph.BenchmarkHealthReady,
		}},
	}
	saved, err := codegraph.SaveBenchmarkReport(report, config.CacheDir())
	if err != nil {
		t.Fatalf("SaveBenchmarkReport: %v", err)
	}

	got := Collect(Options{Version: "test", Config: config.Default()})
	if got.Codegraph.Benchmark.JSONPath == "" {
		t.Fatalf("doctor should expose latest benchmark path: %+v", got.Codegraph.Benchmark)
	}
	if got.Codegraph.Benchmark.JSONPath != redactHome(filepath.Join(filepath.Dir(saved.JSONPath), codegraph.BenchmarkLatestJSONName)) {
		t.Fatalf("benchmark json path = %q, want latest next to %q", got.Codegraph.Benchmark.JSONPath, saved.JSONPath)
	}
	if got.Codegraph.Benchmark.MarkdownPath != redactHome(filepath.Join(filepath.Dir(saved.MarkdownPath), codegraph.BenchmarkLatestMarkdownName)) {
		t.Fatalf("benchmark markdown path = %q, want latest next to %q", got.Codegraph.Benchmark.MarkdownPath, saved.MarkdownPath)
	}
	if len(got.Codegraph.Benchmark.Backends) != 1 || got.Codegraph.Benchmark.Backends[0].ID != "mock" || got.Codegraph.Benchmark.Backends[0].Health != codegraph.BenchmarkHealthReady {
		t.Fatalf("benchmark backends = %+v, want mock ready", got.Codegraph.Benchmark.Backends)
	}

	text := RenderText(got)
	if !strings.Contains(text, "latest bench") || !strings.Contains(text, "mock") || !strings.Contains(text, "ready") {
		t.Fatalf("doctor text missing latest benchmark summary:\n%s", text)
	}
}

func TestCollectReportRedactsBenchmarkReadErrors(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("APPDATA", cacheRoot)
	t.Setenv("XDG_CONFIG_HOME", cacheRoot)
	benchDir := filepath.Join(config.CacheDir(), "codeintel-bench")
	if err := os.MkdirAll(benchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(benchDir, codegraph.BenchmarkLatestJSONName), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Collect(Options{Version: "test", Config: config.Default()})
	if got.Codegraph.Benchmark.Error == "" {
		t.Fatal("expected benchmark parse error")
	}
	if strings.Contains(got.Codegraph.Benchmark.Error, cacheRoot) {
		t.Fatalf("benchmark error leaked cache root: %q", got.Codegraph.Benchmark.Error)
	}
}

func TestCollectReportIncludesEnvironmentSection(t *testing.T) {
	t.Setenv("MADDOG_HOME", filepath.Join(t.TempDir(), "maddog"))
	report := Collect(Options{Version: "1.2.3", Config: config.Default()})
	if len(report.Environment) == 0 {
		t.Fatal("expected environment section in report")
	}
	got := map[string]bool{}
	for _, tool := range report.Environment {
		got[tool.Name] = true
	}
	for _, want := range []string{"wails", "pxpipe", "npx"} {
		if !got[want] {
			t.Fatalf("expected %s in environment section: %+v", want, report.Environment)
		}
	}
	text := RenderText(report)
	if !strings.Contains(text, "environment") {
		t.Fatalf("doctor text missing environment section:\n%s", text)
	}
}

func TestCollectReportIncludesPxpipeMetadataWithoutLogContent(t *testing.T) {
	home := filepath.Join(t.TempDir(), "maddog")
	t.Setenv("MADDOG_HOME", home)
	logPath := filepath.Join(home, "pxpipe", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	secretPrompt := "secret prompt sk-live-pxpipe"
	if err := os.WriteFile(logPath, []byte(secretPrompt), 0o600); err != nil {
		t.Fatal(err)
	}

	report := Collect(Options{Version: "test", Config: config.Default()})
	text := RenderText(report)
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	combined := text + "\n" + string(raw)

	if report.Pxpipe.State == "" || report.Pxpipe.Dashboard == "" {
		t.Fatalf("pxpipe report missing status metadata: %+v", report.Pxpipe)
	}
	if !report.Pxpipe.LogExists || report.Pxpipe.LogBytes != int64(len(secretPrompt)) {
		t.Fatalf("pxpipe log metadata = exists:%v bytes:%d", report.Pxpipe.LogExists, report.Pxpipe.LogBytes)
	}
	if len(report.Pxpipe.Providers) == 0 {
		t.Fatalf("expected pxpipe provider diagnostics: %+v", report.Pxpipe)
	}
	for _, provider := range report.Pxpipe.Providers {
		if !provider.Loopback {
			t.Fatalf("pxpipe provider should point to loopback: %+v", provider)
		}
	}
	if strings.Contains(combined, secretPrompt) || strings.Contains(combined, "sk-live-pxpipe") {
		t.Fatalf("doctor leaked pxpipe event content:\n%s", combined)
	}
}
