package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"maddog/internal/plugin"
	"maddog/internal/tool"
)

type MCPBenchmarkBackend struct {
	path           string
	host           *plugin.Host
	searchTool     tool.Tool
	err            error
	readinessCases []BenchmarkCase
	pollInterval   time.Duration
}

func NewMCPBenchmarkBackend(path string, readinessCases []BenchmarkCase) *MCPBenchmarkBackend {
	return &MCPBenchmarkBackend{
		path:           path,
		readinessCases: readinessCases,
		pollInterval:   300 * time.Millisecond,
	}
}

func (b *MCPBenchmarkBackend) BenchmarkInfo() BenchmarkBackendInfo {
	health := BenchmarkHealthReady
	if b.err != nil {
		health = BackendHealthDegraded
	} else if _, ok := b.resolveLauncher(); !ok {
		health = BackendHealthDegraded
	}
	return BenchmarkBackendInfo{
		ID:   BuiltInBackendID,
		Name: "CodeGraph",
		Capabilities: BackendCapabilities{
			SymbolSearch: true,
		},
		Health: health,
	}
}

func (b *MCPBenchmarkBackend) BuildIndex(ctx context.Context, root string) error {
	if b.searchTool != nil {
		return nil
	}
	bin, ok := b.resolveLauncher()
	if !ok {
		b.err = fmt.Errorf("codegraph is not installed")
		return b.err
	}
	if !IndexableRoot(root) {
		b.err = fmt.Errorf("codegraph: refusing to benchmark non-indexable root %q", root)
		return b.err
	}
	if err := EnsureInit(ctx, bin, root); err != nil {
		b.err = err
		return err
	}
	host := plugin.NewHost()
	tools, err := host.Add(ctx, MCPSpec(bin, root))
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

func (b *MCPBenchmarkBackend) UpdateIndex(ctx context.Context, root string) error {
	if err := b.BuildIndex(ctx, root); err != nil {
		return err
	}
	return b.waitForReadiness(ctx)
}

func (b *MCPBenchmarkBackend) Query(ctx context.Context, query BenchmarkQuery) ([]BenchmarkResult, error) {
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
	return []BenchmarkResult{{
		ID:      "codegraph_search",
		Title:   query.Text,
		Content: out,
	}}, nil
}

func (b *MCPBenchmarkBackend) Close() error {
	if b.host != nil {
		b.host.Close()
		b.host = nil
		b.searchTool = nil
	}
	return nil
}

func (b *MCPBenchmarkBackend) resolveLauncher() (string, bool) {
	if strings.TrimSpace(b.path) != "" {
		if info, err := os.Stat(b.path); err == nil && !info.IsDir() {
			return b.path, true
		}
		return "", false
	}
	return Resolve("")
}

func (b *MCPBenchmarkBackend) waitForReadiness(ctx context.Context) error {
	for _, tc := range b.readinessCases {
		if tc.Capability != BenchmarkCapabilitySymbolSearch || len(tc.ExpectedIDs) == 0 {
			continue
		}
		for {
			out, err := b.executeSearch(ctx, tc.Query)
			if err == nil && containsAllBenchmarkNeedles(out, tc.ExpectedIDs) {
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

func (b *MCPBenchmarkBackend) executeSearch(ctx context.Context, query string) (string, error) {
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

func containsAllBenchmarkNeedles(haystack string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
