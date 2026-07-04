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

type strictMappedBenchmarkTool struct {
	name string
	args json.RawMessage
}

func (t *strictMappedBenchmarkTool) Name() string        { return t.name }
func (t *strictMappedBenchmarkTool) Description() string { return "strict mapped benchmark tool" }
func (t *strictMappedBenchmarkTool) ReadOnly() bool      { return true }
func (t *strictMappedBenchmarkTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`)
}
func (t *strictMappedBenchmarkTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	t.args = append(json.RawMessage(nil), args...)
	return "strict result", nil
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

func TestMappedMCPBenchmarkBackendShapesArgsToStrictSchema(t *testing.T) {
	reg := tool.NewRegistry()
	strictTool := &strictMappedBenchmarkTool{name: "mcp__serena__find_symbol"}
	reg.Add(strictTool)
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

	if _, err := backend.Query(context.Background(), BenchmarkQuery{Text: "PortableSymbol", Capability: BenchmarkCapabilitySymbolSearch, TopK: 5}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got, want := string(strictTool.args), `{"name":"PortableSymbol"}`; got != want {
		t.Fatalf("strict tool args = %s, want %s", got, want)
	}
}

func TestMappedMCPBenchmarkBackendRejectsUnsupportedRequiredSchemaKey(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(schemaOnlyTool{
		name:   "mcp__serena__find_symbol",
		schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
	})
	backend := NewMappedMCPBenchmarkBackend(Backend{
		ID:           "serena",
		Name:         "Serena",
		Kind:         BackendKindMCP,
		ServerName:   "serena",
		ToolMapping:  map[string]string{"symbol_search": "mcp__serena__find_symbol"},
		Capabilities: BackendCapabilities{SymbolSearch: true},
	}, reg)

	_, err := backend.Query(context.Background(), BenchmarkQuery{Text: "PortableSymbol", Capability: BenchmarkCapabilitySymbolSearch})
	if err == nil || !strings.Contains(err.Error(), `required argument "path"`) {
		t.Fatalf("Query err = %v, want unsupported required key", err)
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

type schemaOnlyTool struct {
	name   string
	schema json.RawMessage
}

func (t schemaOnlyTool) Name() string                                             { return t.name }
func (t schemaOnlyTool) Description() string                                      { return "schema only tool" }
func (t schemaOnlyTool) ReadOnly() bool                                           { return true }
func (t schemaOnlyTool) Schema() json.RawMessage                                  { return t.schema }
func (t schemaOnlyTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
