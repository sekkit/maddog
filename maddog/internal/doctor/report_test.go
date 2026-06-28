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

func TestCollectReportIncludesAuthModeAndOfficialProfileWithoutToken(t *testing.T) {
	t.Setenv("OPENAI_ACCESS_TOKEN", "sk-official-secret")

	cfg := config.Default()
	cfg.DefaultModel = "openai-official"
	cfg.Providers = []config.ProviderEntry{{
		Name:                  "openai-official",
		Kind:                  "openai",
		BaseURL:               "https://api.openai.com/v1",
		Model:                 "gpt-5",
		AuthType:              "official_auth",
		BearerTokenEnv:        "OPENAI_ACCESS_TOKEN",
		OfficialAuthProfileID: "openai-desktop",
	}}

	report := Collect(Options{Version: "test-version", Config: cfg})
	if len(report.Providers) != 1 {
		t.Fatalf("providers = %+v", report.Providers)
	}
	p := report.Providers[0]
	if p.AuthMode != "official_auth" || p.CredentialEnv != "OPENAI_ACCESS_TOKEN" || p.OfficialAuthProfileID != "openai-desktop" {
		t.Fatalf("provider auth diagnostics = %+v", p)
	}
	text := RenderText(report)
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	combined := text + "\n" + string(raw)
	if !strings.Contains(combined, "official_auth") || !strings.Contains(combined, "openai-desktop") {
		t.Fatalf("doctor report missing auth diagnostics:\n%s", combined)
	}
	if strings.Contains(combined, "sk-official-secret") {
		t.Fatalf("doctor report leaked official token:\n%s", combined)
	}
}

func TestCollectReportDoesNotRequireAPIKey(t *testing.T) {
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

func TestCollectReportIncludesRecentCodeintelBenchmarkSummary(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("APPDATA", configRoot)

	report := codegraph.BenchmarkReport{
		GeneratedAt: "2026-06-28T09:00:00Z",
		Backends: []codegraph.BackendBenchmarkReport{{
			BackendID:          "builtin-codegraph",
			BackendName:        "CodeGraph",
			Health:             codegraph.BackendHealthAvailable,
			TopKRelevance:      0.75,
			ToolFailures:       1,
			Unsupported:        2,
			TokenCharsReturned: 128,
		}},
	}
	path, err := codegraph.WriteLatestBenchmarkReport(config.CacheDir(), report)
	if err != nil {
		t.Fatalf("WriteLatestBenchmarkReport: %v", err)
	}

	got := Collect(Options{Version: "bench-test", Config: config.Default()})
	if got.Codegraph.RecentBench == nil {
		t.Fatalf("recent benchmark summary missing: %+v", got.Codegraph)
	}
	if got.Codegraph.RecentBench.Path == "" || strings.Contains(got.Codegraph.RecentBench.Path, configRoot) {
		t.Fatalf("recent benchmark path should be present and redacted, got %+v (raw path %s)", got.Codegraph.RecentBench, path)
	}
	if len(got.Codegraph.RecentBench.Backends) != 1 || got.Codegraph.RecentBench.Backends[0].BackendID != "builtin-codegraph" {
		t.Fatalf("recent benchmark backends = %+v", got.Codegraph.RecentBench.Backends)
	}
	text := RenderText(got)
	if !strings.Contains(text, "recent bench") || !strings.Contains(text, "builtin-codegraph") || !strings.Contains(text, "failures:1") {
		t.Fatalf("text report should surface recent bench summary:\n%s", text)
	}
}

func TestCollectReportIncludesHybridStoreSpikeAssessment(t *testing.T) {
	got := Collect(Options{Version: "hybrid-test", Config: config.Default()})
	if got.Codegraph.HybridStoreSpike == nil {
		t.Fatalf("hybrid store spike missing: %+v", got.Codegraph)
	}
	if got.Codegraph.HybridStoreSpike.CandidateID != "zvec" || !got.Codegraph.HybridStoreSpike.DefaultEnabled {
		t.Fatalf("hybrid store spike should be zvec and default-enabled for v1: %+v", got.Codegraph.HybridStoreSpike)
	}
	text := RenderText(got)
	if !strings.Contains(text, "hybrid store") || !strings.Contains(text, "zvec") || !strings.Contains(text, "default:true") {
		t.Fatalf("text report should surface hybrid store spike:\n%s", text)
	}
}
