package codegraph

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maddog/internal/tool"
)

type mappedBenchmarkTool struct {
	name string
}

func (t mappedBenchmarkTool) Name() string        { return t.name }
func (t mappedBenchmarkTool) Description() string { return "mapped benchmark tool" }
func (t mappedBenchmarkTool) ReadOnly() bool      { return true }
func (t mappedBenchmarkTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t mappedBenchmarkTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in map[string]any
	_ = json.Unmarshal(args, &in)
	return "mapped result for " + in["query"].(string), nil
}

func TestMappedMCPBenchmarkBackendRoutesConfiguredTool(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mappedBenchmarkTool{name: "mcp__serena__find_symbol"})
	backend := NewMappedMCPBenchmarkBackend(Backend{
		ID:          "serena",
		Name:        "Serena",
		Kind:        BackendKindMCP,
		ServerName:  "serena",
		ToolMapping: map[string]string{"symbol_search": "mcp__serena__find_symbol"},
		Capabilities: BackendCapabilities{
			SymbolSearch: true,
		},
	}, reg)

	if info := backend.BenchmarkInfo(); info.Health != BenchmarkHealthReady || !info.Capabilities.SymbolSearch {
		t.Fatalf("BenchmarkInfo = %+v, want ready symbol backend", info)
	}
	results, err := backend.Query(context.Background(), BenchmarkQuery{Text: "PortableSymbol", Capability: BenchmarkCapabilitySymbolSearch})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 || !strings.Contains(results[0].Content, "PortableSymbol") {
		t.Fatalf("results = %+v, want mapped tool output", results)
	}
}

func TestMappedMCPBenchmarkBackendDegradesWhenMappedToolMissing(t *testing.T) {
	backend := NewMappedMCPBenchmarkBackend(Backend{
		ID:          "serena",
		Name:        "Serena",
		Kind:        BackendKindMCP,
		ServerName:  "serena",
		ToolMapping: map[string]string{"symbol_search": "mcp__serena__missing"},
		Capabilities: BackendCapabilities{
			SymbolSearch: true,
		},
	}, tool.NewRegistry())

	if info := backend.BenchmarkInfo(); info.Health != BackendHealthDegraded {
		t.Fatalf("BenchmarkInfo = %+v, want degraded for missing mapped tool", info)
	}
	if _, err := backend.Query(context.Background(), BenchmarkQuery{Text: "PortableSymbol", Capability: BenchmarkCapabilitySymbolSearch}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("Query err = %v, want missing mapped tool", err)
	}
}
