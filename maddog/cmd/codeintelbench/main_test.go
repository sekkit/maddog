package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/codegraph"
)

func TestRunCodeIntelBenchDefaultReportExcludesMockBackend(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "alpha.go"), []byte("package fixture\nfunc PortableSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runCodeIntelBench([]string{"-repo", repo, "-out-dir", outDir, "-codegraph-path", filepath.Join(repo, "missing-codegraph")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "latest.json") {
		t.Fatalf("stdout should mention latest report path, got %q", stdout.String())
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "codeintel-bench", codegraph.BenchmarkLatestJSONName))
	if err != nil {
		t.Fatalf("latest json missing: %v", err)
	}
	var report codegraph.BenchmarkReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode latest json: %v", err)
	}
	got := map[string]bool{}
	health := map[string]string{}
	for _, backend := range report.Backends {
		got[backend.ID] = true
		health[backend.ID] = backend.Health
	}
	if got["mock"] {
		t.Fatalf("default benchmark report must not include mock backend: %+v", report.Backends)
	}
	if !got[codegraph.BuiltInBackendID] {
		t.Fatalf("backends = %+v, want built-in codegraph", report.Backends)
	}
	if health[codegraph.BuiltInBackendID] != codegraph.BackendHealthDegraded {
		t.Fatalf("built-in codegraph health = %q, want degraded when launcher is missing", health[codegraph.BuiltInBackendID])
	}
	if _, err := os.Stat(filepath.Join(outDir, "codeintel-bench", codegraph.BenchmarkLatestMarkdownName)); err != nil {
		t.Fatalf("latest markdown missing: %v", err)
	}
	for _, backend := range report.Backends {
		for _, query := range backend.Queries {
			if query.Query == "RunBenchmark" {
				t.Fatalf("default benchmark used Maddog-specific query in non-Maddog repo: %+v", query)
			}
		}
	}
}

func TestRunCodeIntelBenchIncludesConfiguredHyperGraphRAGBackend(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "runner.go"), []byte("package fixture\nfunc RunBenchmark() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "notes.md"), []byte("# Portable Architecture\n\nHyperGraphRAG semantic fixture.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configBody := `
[[code_intelligence.backends]]
name = "project-hypergraph"
kind = "hypergraphrag"
command = "` + strings.ReplaceAll(os.Args[0], `\`, `\\`) + `"
args = ["-test.run=TestHyperGraphRAGSidecarHelperProcess", "--"]
[code_intelligence.backends.env]
GO_WANT_HYPERGRAPHRAG_HELPER_PROCESS = "1"
`
	if err := os.WriteFile(filepath.Join(repo, "maddog.toml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runCodeIntelBench([]string{"-repo", repo, "-out-dir", outDir, "-codegraph-path", filepath.Join(repo, "missing-codegraph")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "codeintel-bench", codegraph.BenchmarkLatestJSONName))
	if err != nil {
		t.Fatalf("latest json missing: %v", err)
	}
	var report codegraph.BenchmarkReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode latest json: %v", err)
	}
	seen := map[string]codegraph.BenchmarkBackendReport{}
	for _, backend := range report.Backends {
		seen[backend.ID] = backend
	}
	hyper, ok := seen["project-hypergraph"]
	if !ok {
		t.Fatalf("HyperGraphRAG backend missing from report: %+v", report.Backends)
	}
	if !hyper.Capabilities.SemanticSearch || hyper.Health != codegraph.BenchmarkHealthReady {
		t.Fatalf("HyperGraphRAG report = %+v, want semantic ready", hyper)
	}
	foundSemantic := false
	for _, query := range hyper.Queries {
		if query.Capability == codegraph.BenchmarkCapabilitySemanticSearch {
			foundSemantic = true
			if query.Status != codegraph.BenchmarkQueryOK || query.RelevanceScore != 1 {
				t.Fatalf("semantic query = %+v, want ok/relevant", query)
			}
		}
	}
	if !foundSemantic {
		t.Fatalf("HyperGraphRAG report missing semantic query: %+v", hyper.Queries)
	}
}

func TestCodeGraphBenchmarkUpdateWaitsForReadinessFixture(t *testing.T) {
	search := &delayedSearchTool{outputs: []string{"index pending", "runner.go: func RunBenchmark()"}}
	backend := &codeGraphMCPBenchmarkBackend{
		searchTool: search,
		readinessCases: []codegraph.BenchmarkCase{{
			Name:        "symbol search",
			Query:       "RunBenchmark",
			Capability:  codegraph.BenchmarkCapabilitySymbolSearch,
			ExpectedIDs: []string{"runner.go"},
		}},
		pollInterval: time.Millisecond,
	}

	if err := backend.UpdateIndex(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}
	if search.calls < 2 {
		t.Fatalf("readiness should poll until expected marker appears, got %d call(s)", search.calls)
	}
}

func TestHyperGraphRAGSidecarHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HYPERGRAPHRAG_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			args = args[i+1:]
			break
		}
	}
	enc := json.NewEncoder(os.Stdout)
	switch args[0] {
	case "health":
		_ = enc.Encode(map[string]string{"status": codegraph.BenchmarkHealthReady})
	case "index":
		_ = enc.Encode(map[string]bool{"indexed": true})
	case "query":
		_ = enc.Encode(map[string]any{"results": []codegraph.BenchmarkResult{{
			ID:      "docs/notes.md",
			Title:   "Portable Architecture",
			Content: "Portable Architecture semantic evidence",
		}}})
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

type delayedSearchTool struct {
	outputs []string
	calls   int
}

func (d *delayedSearchTool) Name() string        { return "mcp__codegraph__search" }
func (d *delayedSearchTool) Description() string { return "search" }
func (d *delayedSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (d *delayedSearchTool) ReadOnly() bool { return true }
func (d *delayedSearchTool) Execute(context.Context, json.RawMessage) (string, error) {
	d.calls++
	if d.calls <= len(d.outputs) {
		return d.outputs[d.calls-1], nil
	}
	return d.outputs[len(d.outputs)-1], nil
}
