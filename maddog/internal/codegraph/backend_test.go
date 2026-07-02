package codegraph

import (
	"strings"
	"testing"

	"maddog/internal/config"
)

func TestBackendRegistryIncludesBuiltInCodeGraph(t *testing.T) {
	cfg := config.Default()
	cfg.Codegraph.Enabled = true

	reg := NewBackendRegistry(cfg)
	backends := reg.Backends()
	if len(backends) != 1 {
		t.Fatalf("Backends count = %d, want 1", len(backends))
	}
	got := backends[0]
	if got.ID != BuiltInBackendID || got.Kind != BackendKindBuiltIn || got.Name != "CodeGraph" {
		t.Fatalf("built-in backend = %+v, want CodeGraph built-in", got)
	}
	if got.Health.Status != BackendHealthReady {
		t.Fatalf("built-in health = %q, want ready", got.Health.Status)
	}
	if !got.Capabilities.SymbolSearch || !got.Capabilities.ContextPack || !got.Capabilities.GraphTrace || !got.Capabilities.Health {
		t.Fatalf("built-in capabilities = %+v, want symbol/context/graph/health", got.Capabilities)
	}
	if got.ToolMapping["context_pack"] != "mcp__codegraph__context" {
		t.Fatalf("context tool = %q, want stripped CodeGraph MCP name", got.ToolMapping["context_pack"])
	}
}

func TestBackendRegistryIncludesExternalMCPBackend(t *testing.T) {
	enabled := true
	cfg := config.Default()
	cfg.Codegraph.Enabled = true
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:    "serena",
		Kind:    "mcp",
		Server:  "serena",
		Enabled: &enabled,
		Tools: map[string]string{
			"symbol_search": "mcp__serena__find_symbol",
			"context_pack":  "mcp__serena__read_context",
			"health":        "mcp__serena__status",
		},
	}}

	reg := NewBackendRegistry(cfg)
	backends := reg.Backends()
	if len(backends) != 2 {
		t.Fatalf("Backends count = %d, want built-in + external: %+v", len(backends), backends)
	}
	got, ok := reg.Backend("serena")
	if !ok {
		t.Fatalf("serena backend missing from registry: %+v", backends)
	}
	if got.Kind != BackendKindMCP || got.ServerName != "serena" {
		t.Fatalf("serena backend = %+v, want MCP server mapping", got)
	}
	if got.Health.Status != BackendHealthDegraded {
		t.Fatalf("external health = %q, want degraded before MCP connects", got.Health.Status)
	}
	if !got.Capabilities.SymbolSearch || !got.Capabilities.ContextPack || !got.Capabilities.Health {
		t.Fatalf("external capabilities = %+v, want derived from tool mapping", got.Capabilities)
	}
}

func TestBackendRegistryIncludesHyperGraphRAGBackend(t *testing.T) {
	enabled := true
	cfg := config.Default()
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:    "project-hypergraph",
		Kind:    "hypergraphrag",
		Command: "maddog-hypergraphrag",
		Args:    []string{"--workdir", ".maddog/hypergraph"},
		Enabled: &enabled,
		Env: map[string]string{
			"OPENAI_API_KEY": "${OPENAI_API_KEY}",
		},
	}}

	reg := NewBackendRegistry(cfg)
	got, ok := reg.Backend("project-hypergraph")
	if !ok {
		t.Fatalf("HyperGraphRAG backend missing from registry: %+v invalid=%+v", reg.Backends(), reg.InvalidBackends())
	}
	if got.Kind != BackendKindHyperGraphRAG {
		t.Fatalf("kind = %q, want %q", got.Kind, BackendKindHyperGraphRAG)
	}
	if got.Health.Status != BackendHealthDegraded {
		t.Fatalf("health = %q, want degraded until sidecar health is checked", got.Health.Status)
	}
	if got.Command != "maddog-hypergraphrag" || len(got.Args) != 2 || got.Args[1] != ".maddog/hypergraph" {
		t.Fatalf("sidecar command not preserved: %+v", got)
	}
	if !got.Capabilities.SemanticSearch || !got.Capabilities.ContextPack || !got.Capabilities.Health {
		t.Fatalf("capabilities = %+v, want semantic/context/health", got.Capabilities)
	}
	if len(got.ToolMapping) != 0 {
		t.Fatalf("HyperGraphRAG sidecar should not require MCP tool mapping, got %+v", got.ToolMapping)
	}
}

func TestBackendRegistryRejectsHyperGraphRAGWithoutCommand(t *testing.T) {
	cfg := config.Default()
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name: "project-hypergraph",
		Kind: "hypergraphrag",
	}}

	reg := NewBackendRegistry(cfg)
	if _, ok := reg.Backend("project-hypergraph"); ok {
		t.Fatal("HyperGraphRAG backend without command should not be registered as usable")
	}
	invalid := reg.InvalidBackends()
	if len(invalid) != 1 || invalid[0].ID != "project-hypergraph" {
		t.Fatalf("InvalidBackends = %+v, want project-hypergraph", invalid)
	}
	if invalid[0].Health.Status != BackendHealthInvalid || !strings.Contains(invalid[0].Health.Error, "command") {
		t.Fatalf("invalid health = %+v, want missing command error", invalid[0].Health)
	}
}

func TestBackendRegistryMarksInvalidExternalBackend(t *testing.T) {
	enabled := true
	cfg := config.Default()
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:    "broken",
		Kind:    "mcp",
		Server:  "broken",
		Enabled: &enabled,
		Tools:   map[string]string{},
	}}

	reg := NewBackendRegistry(cfg)
	if _, ok := reg.Backend("broken"); ok {
		t.Fatal("invalid backend should not be registered as usable")
	}
	invalid := reg.InvalidBackends()
	if len(invalid) != 1 || invalid[0].ID != "broken" {
		t.Fatalf("InvalidBackends = %+v, want broken", invalid)
	}
	if invalid[0].Health.Status != BackendHealthInvalid || invalid[0].Health.Error == "" {
		t.Fatalf("invalid health = %+v, want invalid with error", invalid[0].Health)
	}
}

func TestBackendRegistryMarksEmptyToolMappingValueInvalid(t *testing.T) {
	cfg := config.Default()
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:   "serena",
		Kind:   "mcp",
		Server: "serena",
		Tools: map[string]string{
			"context_pack": "",
		},
	}}

	reg := NewBackendRegistry(cfg)
	if _, ok := reg.Backend("serena"); ok {
		t.Fatal("backend with empty tool mapping value should not be registered as usable")
	}
	invalid := reg.InvalidBackends()
	if len(invalid) != 1 || invalid[0].ID != "serena" {
		t.Fatalf("InvalidBackends = %+v, want serena", invalid)
	}
	if invalid[0].Health.Status != BackendHealthInvalid || !strings.Contains(invalid[0].Health.Error, "context_pack") {
		t.Fatalf("invalid health = %+v, want context_pack error", invalid[0].Health)
	}
}

func TestBackendRegistryRejectsExternalBackendReservedBuiltInID(t *testing.T) {
	cfg := config.Default()
	cfg.Codegraph.Enabled = true
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:   BuiltInBackendID,
		Kind:   "mcp",
		Server: "serena",
		Tools: map[string]string{
			"context_pack": "mcp__serena__context",
		},
	}}

	reg := NewBackendRegistry(cfg)
	got, ok := reg.Backend(BuiltInBackendID)
	if !ok {
		t.Fatal("built-in CodeGraph backend missing")
	}
	if got.Kind != BackendKindBuiltIn || got.ServerName != "codegraph" || got.ToolMapping["context_pack"] != "mcp__codegraph__context" {
		t.Fatalf("reserved external backend replaced built-in CodeGraph: %+v", got)
	}
	invalid := reg.InvalidBackends()
	if len(invalid) != 1 || invalid[0].ID != BuiltInBackendID {
		t.Fatalf("InvalidBackends = %+v, want reserved codegraph entry", invalid)
	}
	if invalid[0].Health.Status != BackendHealthInvalid || !strings.Contains(invalid[0].Health.Error, "reserved") {
		t.Fatalf("invalid reserved health = %+v, want reserved-name error", invalid[0].Health)
	}
}
