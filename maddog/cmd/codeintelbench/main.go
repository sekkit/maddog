package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"maddog/internal/codegraph"
	"maddog/internal/config"
	"maddog/internal/plugin"
	"maddog/internal/tool"
)

func main() {
	os.Exit(runCodeIntelBench(os.Args[1:], os.Stdout, os.Stderr))
}

func runCodeIntelBench(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("codeintelbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "repository root to benchmark")
	outDir := fs.String("out-dir", config.CacheDir(), "directory where codeintel-bench reports are written")
	codegraphPath := fs.String("codegraph-path", "", "optional CodeGraph launcher path override")
	timeout := fs.Duration("timeout", 2*time.Minute, "benchmark timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*outDir) == "" {
		fmt.Fprintln(stderr, "out-dir is required when Maddog cache dir is unavailable")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cases := defaultBenchmarkCases()
	report := codegraph.RunBenchmark(ctx, codegraph.BenchmarkOptions{
		Root: *repo,
		Backends: []codegraph.BenchmarkBackend{
			mockBenchmarkBackend{},
			newCodeGraphMCPBenchmarkBackend(*codegraphPath, cases),
		},
		Cases: cases,
	})
	saved, err := codegraph.SaveBenchmarkReport(report, *outDir)
	if err != nil {
		fmt.Fprintf(stderr, "save benchmark report: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "code intelligence benchmark written:\n")
	fmt.Fprintf(stdout, "  json: %s\n", saved.JSONPath)
	fmt.Fprintf(stdout, "  markdown: %s\n", saved.MarkdownPath)
	fmt.Fprintf(stdout, "  latest: %s\n", codegraph.BenchmarkLatestJSONName)
	return 0
}

func defaultBenchmarkCases() []codegraph.BenchmarkCase {
	return []codegraph.BenchmarkCase{{
		Name:        "symbol search",
		Query:       "RunBenchmark",
		Capability:  codegraph.BenchmarkCapabilitySymbolSearch,
		ExpectedIDs: []string{"runner.go"},
		TopK:        5,
	}}
}

type mockBenchmarkBackend struct{}

func (mockBenchmarkBackend) BenchmarkInfo() codegraph.BenchmarkBackendInfo {
	return codegraph.BenchmarkBackendInfo{
		ID:   "mock",
		Name: "MockGraph",
		Capabilities: codegraph.BackendCapabilities{
			SymbolSearch: true,
		},
		Health: codegraph.BenchmarkHealthReady,
	}
}

func (mockBenchmarkBackend) BuildIndex(context.Context, string) error { return nil }

func (mockBenchmarkBackend) UpdateIndex(context.Context, string) error { return nil }

func (mockBenchmarkBackend) Query(_ context.Context, query codegraph.BenchmarkQuery) ([]codegraph.BenchmarkResult, error) {
	if query.Capability != codegraph.BenchmarkCapabilitySymbolSearch {
		return nil, nil
	}
	return []codegraph.BenchmarkResult{{
		ID:      "runner.go",
		Title:   "RunBenchmark",
		Content: "func RunBenchmark(...)",
	}}, nil
}

type codeGraphMCPBenchmarkBackend struct {
	path           string
	host           *plugin.Host
	searchTool     tool.Tool
	err            error
	readinessCases []codegraph.BenchmarkCase
	pollInterval   time.Duration
}

func newCodeGraphMCPBenchmarkBackend(path string, readinessCases []codegraph.BenchmarkCase) *codeGraphMCPBenchmarkBackend {
	return &codeGraphMCPBenchmarkBackend{
		path:           path,
		readinessCases: readinessCases,
		pollInterval:   300 * time.Millisecond,
	}
}

func (b *codeGraphMCPBenchmarkBackend) BenchmarkInfo() codegraph.BenchmarkBackendInfo {
	health := codegraph.BenchmarkHealthReady
	if b.err != nil {
		health = codegraph.BackendHealthDegraded
	} else if _, ok := b.resolveLauncher(); !ok {
		health = codegraph.BackendHealthDegraded
	}
	return codegraph.BenchmarkBackendInfo{
		ID:   codegraph.BuiltInBackendID,
		Name: "CodeGraph",
		Capabilities: codegraph.BackendCapabilities{
			SymbolSearch: true,
		},
		Health: health,
	}
}

func (b *codeGraphMCPBenchmarkBackend) BuildIndex(ctx context.Context, root string) error {
	if b.searchTool != nil {
		return nil
	}
	bin, ok := b.resolveLauncher()
	if !ok {
		b.err = fmt.Errorf("codegraph is not installed")
		return b.err
	}
	if !codegraph.IndexableRoot(root) {
		b.err = fmt.Errorf("codegraph: refusing to benchmark non-indexable root %q", root)
		return b.err
	}
	if err := codegraph.EnsureInit(ctx, bin, root); err != nil {
		b.err = err
		return err
	}
	host := plugin.NewHost()
	tools, err := host.Add(ctx, codegraph.MCPSpec(bin, root))
	if err != nil {
		host.Close()
		b.err = err
		return err
	}
	for _, tl := range tools {
		if tl.Name() == "mcp__codegraph__search" {
			b.host = host
			b.searchTool = tl
			b.err = nil
			return nil
		}
	}
	host.Close()
	b.err = fmt.Errorf("codegraph search tool not available")
	return b.err
}

func (b *codeGraphMCPBenchmarkBackend) resolveLauncher() (string, bool) {
	if strings.TrimSpace(b.path) != "" {
		if info, err := os.Stat(b.path); err == nil && !info.IsDir() {
			return b.path, true
		}
		return "", false
	}
	return codegraph.Resolve("")
}

func (b *codeGraphMCPBenchmarkBackend) UpdateIndex(ctx context.Context, root string) error {
	if err := b.BuildIndex(ctx, root); err != nil {
		return err
	}
	return b.waitForReadiness(ctx)
}

func (b *codeGraphMCPBenchmarkBackend) Query(ctx context.Context, query codegraph.BenchmarkQuery) ([]codegraph.BenchmarkResult, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.searchTool == nil {
		return nil, fmt.Errorf("codegraph search tool not initialized")
	}
	out, err := b.executeSearch(ctx, query.Text)
	if err != nil {
		return nil, err
	}
	return []codegraph.BenchmarkResult{{
		ID:      "codegraph_search",
		Title:   query.Text,
		Content: out,
	}}, nil
}

func (b *codeGraphMCPBenchmarkBackend) waitForReadiness(ctx context.Context) error {
	for _, tc := range b.readinessCases {
		if tc.Capability != codegraph.BenchmarkCapabilitySymbolSearch || len(tc.ExpectedIDs) == 0 {
			continue
		}
		for {
			out, err := b.executeSearch(ctx, tc.Query)
			if err == nil && containsAll(out, tc.ExpectedIDs) {
				break
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			interval := b.pollInterval
			if interval <= 0 {
				interval = 300 * time.Millisecond
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

func (b *codeGraphMCPBenchmarkBackend) executeSearch(ctx context.Context, query string) (string, error) {
	if b.err != nil {
		return "", b.err
	}
	if b.searchTool == nil {
		return "", fmt.Errorf("codegraph search tool not initialized")
	}
	raw, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		return "", err
	}
	return b.searchTool.Execute(ctx, raw)
}

func containsAll(haystack string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func (b *codeGraphMCPBenchmarkBackend) Close() error {
	if b.host != nil {
		b.host.Close()
		b.host = nil
		b.searchTool = nil
	}
	return nil
}
