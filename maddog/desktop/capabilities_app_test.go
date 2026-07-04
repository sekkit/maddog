package main

import (
	"os"
	"path/filepath"
	"testing"

	"maddog/internal/codegraph"
	"maddog/internal/config"
	"maddog/internal/control"
	"maddog/internal/plugin"
)

func TestCapabilitiesProjectsBuiltInCodeIntelligenceBackend(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	if err := os.Mkdir(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	got, ok := findCodeIntelligenceBackend(view.CodeIntelligenceBackends, "codegraph")
	if !ok {
		t.Fatalf("CodeGraph backend missing from Capabilities: %+v", view.CodeIntelligenceBackends)
	}
	if got.ID != codegraph.BuiltInBackendID || got.Name != "CodeGraph" || got.Kind != codegraph.BackendKindBuiltIn || got.ServerName != "codegraph" {
		t.Fatalf("CodeGraph backend identity = %+v, want built-in CodeGraph", got)
	}
	if !got.Enabled || !got.BuiltIn || !got.Configured || got.Status != codegraph.BackendHealthReady || got.LastError != "" {
		t.Fatalf("CodeGraph backend state = %+v, want enabled built-in configured ready with no error", got)
	}
	if got.ToolCount != 4 || got.ToolMapping["context_pack"] != "mcp__codegraph__context" {
		t.Fatalf("CodeGraph tools = count %d mapping %+v, want context tool mapping", got.ToolCount, got.ToolMapping)
	}
	if got.IndexStatus != "initialized" {
		t.Fatalf("CodeGraph index status = %q, want initialized", got.IndexStatus)
	}
	if !got.Capabilities.SymbolSearch || !got.Capabilities.ContextPack || !got.Capabilities.GraphTrace || !got.Capabilities.Health {
		t.Fatalf("CodeGraph capabilities = %+v, want symbol/context/graph/health", got.Capabilities)
	}
}

func TestCapabilitiesProjectsConfiguredExternalCodeIntelligenceBackend(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "maddog.toml"), []byte(`
[codegraph]
enabled = true

[[code_intelligence.backends]]
name = "serena"
kind = "mcp"
server = "serena"

[code_intelligence.backends.tools]
symbol_search = "mcp__serena__find_symbol"
context_pack = "mcp__serena__get_symbols_overview"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	if _, ok := findCodeIntelligenceBackend(view.CodeIntelligenceBackends, "codegraph"); !ok {
		t.Fatalf("built-in backend missing when external is configured: %+v", view.CodeIntelligenceBackends)
	}
	got, ok := findCodeIntelligenceBackend(view.CodeIntelligenceBackends, "serena")
	if !ok {
		t.Fatalf("external backend missing from Capabilities: %+v", view.CodeIntelligenceBackends)
	}
	if got.ID != "serena" || got.Kind != codegraph.BackendKindMCP || got.ServerName != "serena" || got.BuiltIn {
		t.Fatalf("external backend identity = %+v, want MCP serena", got)
	}
	if !got.Enabled || !got.Configured || got.Status != codegraph.BackendHealthDegraded || got.LastError != "" {
		t.Fatalf("external backend state = %+v, want configured enabled degraded with no error", got)
	}
	if got.ToolCount != 2 || got.ToolMapping["symbol_search"] != "mcp__serena__find_symbol" {
		t.Fatalf("external tools = count %d mapping %+v, want configured mapping", got.ToolCount, got.ToolMapping)
	}
	if !got.Capabilities.SymbolSearch || !got.Capabilities.ContextPack || got.Capabilities.Health || got.Capabilities.GraphTrace {
		t.Fatalf("external capabilities = %+v, want symbol/context only", got.Capabilities)
	}
}

func TestCapabilitiesProjectsKnownCodeIntelligencePresets(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	serena, ok := findCodeIntelligenceBackend(view.CodeIntelligenceBackends, "serena")
	if !ok {
		t.Fatalf("serena preset missing from Capabilities: %+v", view.CodeIntelligenceBackends)
	}
	if serena.Configured || serena.Enabled || serena.Status != codegraph.BackendHealthUnknown {
		t.Fatalf("serena preset state = %+v, want visible unconfigured disabled unknown preset", serena)
	}
	if serena.ToolMapping["symbol_search"] != "mcp__serena__find_symbol" || !serena.Capabilities.SymbolSearch {
		t.Fatalf("serena preset mapping/capabilities = %+v", serena)
	}
	zvec, ok := findCodeIntelligenceBackend(view.CodeIntelligenceBackends, "zvec")
	if !ok {
		t.Fatalf("zvec research preset missing from Capabilities: %+v", view.CodeIntelligenceBackends)
	}
	if !zvec.ResearchOnly || zvec.Configured || zvec.Kind != "" {
		t.Fatalf("zvec preset = %+v, want research-only non-configured preset", zvec)
	}
}

func TestCodeIntelligenceBackendViewsReflectLiveExternalMCPState(t *testing.T) {
	cfg := codeIntelligenceTestConfig()
	reg := codegraph.NewBackendRegistry(cfg)

	views := codeIntelligenceBackendViews(reg, "", map[string]plugin.ServerStatus{
		"serena": {Name: "serena", Tools: 9},
	}, nil, CodeIntelligenceBenchmarkView{}, nil)
	got, ok := findCodeIntelligenceBackend(views, "serena")
	if !ok {
		t.Fatalf("serena backend missing: %+v", views)
	}
	if got.Status != codegraph.BackendHealthReady || got.ToolCount != 9 || got.LastError != "" {
		t.Fatalf("connected external backend = %+v, want ready with live tool count", got)
	}

	views = codeIntelligenceBackendViews(reg, "", nil, map[string]plugin.Failure{
		"serena": {Name: "serena", Error: "connect refused"},
	}, CodeIntelligenceBenchmarkView{}, nil)
	got, ok = findCodeIntelligenceBackend(views, "serena")
	if !ok {
		t.Fatalf("serena backend missing after failure: %+v", views)
	}
	if got.Status != codegraph.BackendHealthInvalid || got.LastError != "connect refused" {
		t.Fatalf("failed external backend = %+v, want invalid with last error", got)
	}
}

func TestCodeIntelligenceBackendViewsAttachBenchmarkOnlyToMatchingBackend(t *testing.T) {
	cfg := codeIntelligenceTestConfig()
	reg := codegraph.NewBackendRegistry(cfg)

	views := codeIntelligenceBackendViews(reg, "", nil, nil, CodeIntelligenceBenchmarkView{}, nil)
	if got, ok := findCodeIntelligenceBackend(views, "codegraph"); ok && got.Benchmark != nil {
		t.Fatalf("empty benchmark should not be attached to codegraph: %+v", got.Benchmark)
	}
	if got, ok := findCodeIntelligenceBackend(views, "serena"); ok && got.Benchmark != nil {
		t.Fatalf("empty benchmark should not be attached to serena: %+v", got.Benchmark)
	}

	benchmark := CodeIntelligenceBenchmarkView{
		JSONPath: "latest.json",
		Backends: []CodeIntelligenceBenchmarkBackendView{{
			ID:     "serena",
			Health: codegraph.BackendHealthReady,
		}},
	}
	views = codeIntelligenceBackendViews(reg, "", nil, nil, benchmark, nil)
	codegraphBackend, ok := findCodeIntelligenceBackend(views, "codegraph")
	if !ok {
		t.Fatalf("codegraph backend missing: %+v", views)
	}
	if codegraphBackend.Benchmark != nil {
		t.Fatalf("serena benchmark should not attach to codegraph: %+v", codegraphBackend.Benchmark)
	}
	serenaBackend, ok := findCodeIntelligenceBackend(views, "serena")
	if !ok {
		t.Fatalf("serena backend missing: %+v", views)
	}
	if serenaBackend.Benchmark == nil || serenaBackend.Benchmark.JSONPath != "latest.json" {
		t.Fatalf("serena benchmark = %+v, want latest report", serenaBackend.Benchmark)
	}
}

func TestCodeIntelligenceBackendViewsKeepInvalidBackendInvalidWhenServerConnected(t *testing.T) {
	cfg := config.Default()
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:   "serena",
		Kind:   "mcp",
		Server: "serena",
		Tools: map[string]string{
			"context_pack": "",
		},
	}}
	reg := codegraph.NewBackendRegistry(cfg)

	views := codeIntelligenceBackendViews(reg, "", map[string]plugin.ServerStatus{
		"serena": {Name: "serena", Tools: 9},
	}, nil, CodeIntelligenceBenchmarkView{}, nil)
	got, ok := findCodeIntelligenceBackend(views, "serena")
	if !ok {
		t.Fatalf("invalid serena backend missing: %+v", views)
	}
	if got.Status != codegraph.BackendHealthInvalid || got.LastError == "" || got.ToolCount != 1 {
		t.Fatalf("invalid connected backend = %+v, want invalid with mapping count and error", got)
	}
}

func codeIntelligenceTestConfig() *config.Config {
	cfg := config.Default()
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:   "serena",
		Kind:   "mcp",
		Server: "serena",
		Tools: map[string]string{
			"symbol_search": "mcp__serena__find_symbol",
			"context_pack":  "mcp__serena__get_symbols_overview",
		},
	}}
	return cfg
}

func findCodeIntelligenceBackend(backends []CodeIntelligenceBackendView, id string) (CodeIntelligenceBackendView, bool) {
	for _, backend := range backends {
		if backend.ID == id {
			return backend, true
		}
	}
	return CodeIntelligenceBackendView{}, false
}
