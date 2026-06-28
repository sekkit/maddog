package main

import (
	"os"
	"path/filepath"
	"testing"

	"maddog/internal/config"
	"maddog/internal/control"
	"maddog/internal/plugin"
)

func TestCapabilitiesShowsCodeIntelligenceBackends(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeCapabilitiesTestFile(t, dir, "maddog.toml", `
[codegraph]
enabled = true

[[codegraph.backends]]
name = "serena"
server = "serena"
enabled = true
capabilities = ["symbol_search", "semantic_search", "context_pack"]
risks = ["read", "network"]

[codegraph.backends.tool_mapping]
context = "mcp__serena__context"
search = "mcp__serena__search"
`)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	if len(view.CodeBackends) < 2 {
		t.Fatalf("CodeBackends = %+v, want built-in and external", view.CodeBackends)
	}
	builtIn := findCodeBackend(view.CodeBackends, "builtin-codegraph")
	if builtIn == nil || !builtIn.Default || builtIn.Health != "available" || builtIn.ToolMapping["context"] != "mcp__codegraph__context" {
		t.Fatalf("built-in code backend = %+v", builtIn)
	}
	fast := findCodeBackend(view.CodeBackends, "fastcontext-style")
	if fast == nil || fast.Default || !fast.Enabled || fast.Health != "available" || !containsString(fast.Capabilities, "semantic_search") {
		t.Fatalf("FastContext-style code backend = %+v", fast)
	}
	external := findCodeBackend(view.CodeBackends, "serena")
	if external == nil || external.Health != "degraded" || external.LastError == "" || !containsString(external.Risks, "network") {
		t.Fatalf("external code backend = %+v", external)
	}
}

func TestSetCodeBackendEnabledPersistsExternalBackend(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeCapabilitiesTestFile(t, dir, "maddog.toml", `
[codegraph]
enabled = true

[[codegraph.backends]]
name = "serena"
server = "serena"
enabled = false
capabilities = ["symbol_search", "semantic_search"]
risks = ["read", "network"]

[codegraph.backends.tool_mapping]
context = "mcp__serena__context"
search = "mcp__serena__search"
`)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.SetCodeBackendEnabled("serena", true); err != nil {
		t.Fatalf("SetCodeBackendEnabled: %v", err)
	}
	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Codegraph.Backends) != 1 || !cfg.Codegraph.Backends[0].Enabled {
		t.Fatalf("external backend not persisted enabled: %+v", cfg.Codegraph.Backends)
	}
	view := app.Capabilities()
	external := findCodeBackend(view.CodeBackends, "serena")
	if external == nil || !external.Enabled || external.Health != "degraded" {
		t.Fatalf("external backend view = %+v", external)
	}
}

func TestRunCodeBackendBenchmarkStoresSummary(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeCapabilitiesTestFile(t, dir, "maddog.toml", `
[codegraph]
enabled = true
`)
	writeCapabilitiesTestFile(t, dir, "pkg/sample.go", `
package sample

type LoopTemplateV1 struct{}
type ReadinessResult struct{}
`)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	summary, err := app.RunCodeBackendBenchmark("builtin-codegraph")
	if err != nil {
		t.Fatalf("RunCodeBackendBenchmark: %v", err)
	}
	if summary.BackendID != "builtin-codegraph" || summary.Path == "" || summary.GeneratedAt == "" || summary.CitationPrecision < 0 {
		t.Fatalf("benchmark summary = %+v", summary)
	}
	view := app.Capabilities()
	builtIn := findCodeBackend(view.CodeBackends, "builtin-codegraph")
	if builtIn == nil || builtIn.Benchmark == nil || builtIn.Benchmark.Path == "" || builtIn.Benchmark.CitationPrecision < 0 {
		t.Fatalf("capabilities should include recent benchmark summary: %+v", builtIn)
	}
}

func TestRunFastContextStyleBenchmarkStoresSummary(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeCapabilitiesTestFile(t, dir, "maddog.toml", `
[codegraph]
enabled = true
`)
	writeCapabilitiesTestFile(t, dir, "pkg/sample.go", `
package sample

type LoopTemplateV1 struct{}
type ReadinessResult struct{}
`)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	summary, err := app.RunCodeBackendBenchmark("fastcontext-style")
	if err != nil {
		t.Fatalf("RunCodeBackendBenchmark fastcontext-style: %v", err)
	}
	if summary.BackendID != "fastcontext-style" || summary.Path == "" || summary.GeneratedAt == "" || summary.CitationPrecision < 0 {
		t.Fatalf("FastContext-style benchmark summary = %+v", summary)
	}
}

func writeCapabilitiesTestFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findCodeBackend(items []CodeBackendView, id string) *CodeBackendView {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
