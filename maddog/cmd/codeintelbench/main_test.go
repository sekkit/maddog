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

func TestRunCodeIntelBenchWritesComparableLocalReport(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "runner.go"), []byte("package fixture\nfunc RunBenchmark() {}\n"), 0o644); err != nil {
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
	if !got["mock"] || !got[codegraph.BuiltInBackendID] {
		t.Fatalf("backends = %+v, want mock and built-in codegraph", report.Backends)
	}
	if health[codegraph.BuiltInBackendID] != codegraph.BackendHealthDegraded {
		t.Fatalf("built-in codegraph health = %q, want degraded when launcher is missing", health[codegraph.BuiltInBackendID])
	}
	if _, err := os.Stat(filepath.Join(outDir, "codeintel-bench", codegraph.BenchmarkLatestMarkdownName)); err != nil {
		t.Fatalf("latest markdown missing: %v", err)
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
