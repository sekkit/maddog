package codegraph

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/loop"
)

func TestRegistryFromConfigRegistersBuiltInDefaultBackend(t *testing.T) {
	reg := RegistryFromConfig(config.CodegraphConfig{Enabled: true}, RuntimeSnapshot{})

	backends := reg.Backends()
	if len(backends) != 1 {
		t.Fatalf("Backends len = %d, want 1: %+v", len(backends), backends)
	}
	b := backends[0]
	if b.ID != BuiltInBackendID || !b.Default || !b.BuiltIn || b.Health != BackendHealthAvailable {
		t.Fatalf("built-in backend = %+v", b)
	}
	if b.ToolMapping["context"] != "mcp__codegraph__context" || b.ToolMapping["search"] != "mcp__codegraph__search" {
		t.Fatalf("built-in tool mapping = %+v", b.ToolMapping)
	}
	if !hasBackendCapability(b.Capabilities, BackendCapabilityGraphTrace) || !hasRisk(b.Risks, loop.CapabilityRead) || !hasRisk(b.Risks, loop.CapabilityProcess) {
		t.Fatalf("built-in capabilities/risks = %+v / %+v", b.Capabilities, b.Risks)
	}
}

func TestRegistryKeepsBuiltInFallbackWhenExternalBackendDegraded(t *testing.T) {
	reg := RegistryFromConfig(config.CodegraphConfig{
		Enabled: true,
		Backends: []config.CodegraphBackendConfig{{
			Name:         "serena",
			Server:       "serena",
			Enabled:      true,
			Capabilities: []string{"symbol_search", "semantic_search", "context_pack"},
			Risks:        []string{"read", "network"},
			ToolMapping: map[string]string{
				"context": "mcp__serena__context",
				"search":  "mcp__serena__search",
			},
		}},
	}, RuntimeSnapshot{})

	if got := reg.DefaultBackend(); got.ID != BuiltInBackendID {
		t.Fatalf("DefaultBackend = %+v, want built-in fallback", got)
	}
	ext, ok := reg.Backend("serena")
	if !ok {
		t.Fatalf("external backend missing: %+v", reg.Backends())
	}
	if ext.Health != BackendHealthDegraded || ext.LastError == "" {
		t.Fatalf("external backend health = %+v, want degraded with error", ext)
	}
}

func TestRegistryMarksExternalBackendInvalidWhenRequiredToolMappingMissing(t *testing.T) {
	reg := RegistryFromConfig(config.CodegraphConfig{
		Enabled: true,
		Backends: []config.CodegraphBackendConfig{{
			Name:    "bad",
			Server:  "bad-mcp",
			Enabled: true,
			ToolMapping: map[string]string{
				"search": "mcp__bad-mcp__search",
			},
		}},
	}, RuntimeSnapshot{Servers: map[string]RuntimeServer{
		"bad-mcp": {Connected: true, ToolNames: []string{"mcp__bad-mcp__search"}},
	}})

	b, ok := reg.Backend("bad")
	if !ok {
		t.Fatal("invalid backend missing")
	}
	if b.Health != BackendHealthInvalid || b.LastError == "" {
		t.Fatalf("backend = %+v, want invalid mapping", b)
	}
}

func TestBackendRiskCapabilitiesMapToLoopReadiness(t *testing.T) {
	reg := RegistryFromConfig(config.CodegraphConfig{
		Enabled: false,
		Backends: []config.CodegraphBackendConfig{{
			Name:         "networked",
			Server:       "networked",
			Enabled:      true,
			Risks:        []string{"read", "network", "write"},
			ToolMapping:  map[string]string{"context": "mcp__networked__context", "search": "mcp__networked__search"},
			Capabilities: []string{"context_pack"},
		}},
	}, RuntimeSnapshot{Servers: map[string]RuntimeServer{
		"networked": {Connected: true, ToolNames: []string{"mcp__networked__context", "mcp__networked__search"}},
	}})
	b, ok := reg.Backend("networked")
	if !ok {
		t.Fatal("networked backend missing")
	}
	result := loop.EvaluateReadiness(loop.ReadinessInput{
		Template: loop.LoopTemplateV1{
			ID:                   "code-backend",
			ReadinessGates:       []string{"required_code_backend_available"},
			RequiredCapabilities: b.Risks,
		},
		CodeBackendAvailable: true,
		AuthorizedCapabilities: []loop.Capability{
			loop.CapabilityRead,
		},
	})
	if result.Status != loop.ReadinessBlocked || !hasCheck(result, "capability:network", loop.CheckBlocked) || !hasCheck(result, "capability:write", loop.CheckBlocked) {
		t.Fatalf("readiness = %+v, want blocked on network/write", result)
	}
}

func hasBackendCapability(items []BackendCapability, want BackendCapability) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func hasRisk(items []loop.Capability, want loop.Capability) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func hasCheck(result loop.ReadinessResult, id string, status loop.ReadinessCheckStatus) bool {
	for _, check := range result.Checks {
		if check.ID == id && check.Status == status {
			return true
		}
	}
	return false
}
