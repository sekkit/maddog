package codegraph

import (
	"fmt"
	"sort"
	"strings"

	"maddog/internal/config"
	"maddog/internal/loop"
)

const (
	BuiltInBackendID          = "builtin-codegraph"
	FastContextStyleBackendID = "fastcontext-style"
)

type BackendCapability string

const (
	BackendCapabilitySymbolSearch   BackendCapability = "symbol_search"
	BackendCapabilitySemanticSearch BackendCapability = "semantic_search"
	BackendCapabilityContextPack    BackendCapability = "context_pack"
	BackendCapabilityGraphTrace     BackendCapability = "graph_trace"
	BackendCapabilityEditRefactor   BackendCapability = "edit_refactor"
	BackendCapabilityHealth         BackendCapability = "health"
	BackendCapabilityDenseVector    BackendCapability = "dense_vector"
	BackendCapabilitySparseVector   BackendCapability = "sparse_vector"
	BackendCapabilityFullTextSearch BackendCapability = "full_text_search"
	BackendCapabilityHybridSearch   BackendCapability = "hybrid_search"
	BackendCapabilityWAL            BackendCapability = "wal"
)

type BackendHealth string

const (
	BackendHealthAvailable BackendHealth = "available"
	BackendHealthDegraded  BackendHealth = "degraded"
	BackendHealthDisabled  BackendHealth = "disabled"
	BackendHealthInvalid   BackendHealth = "invalid"
)

type Backend struct {
	ID             string
	Name           string
	Server         string
	Kind           string
	Default        bool
	BuiltIn        bool
	Enabled        bool
	Health         BackendHealth
	IndexFreshness string
	ToolCount      int
	LastError      string
	Capabilities   []BackendCapability
	Risks          []loop.Capability
	ToolMapping    map[string]string
}

type RuntimeSnapshot struct {
	Servers map[string]RuntimeServer
}

type RuntimeServer struct {
	Connected bool
	ToolNames []string
	ToolCount int
	Error     string
}

type Registry struct {
	backends []Backend
}

func RegistryFromConfig(cfg config.CodegraphConfig, runtime RuntimeSnapshot) Registry {
	backends := []Backend{builtInBackend(cfg), fastContextStyleBackend(cfg)}
	for _, external := range cfg.Backends {
		backends = append(backends, externalBackend(external, runtime))
	}
	return Registry{backends: backends}
}

func (r Registry) Backends() []Backend {
	out := make([]Backend, len(r.backends))
	copy(out, r.backends)
	return out
}

func (r Registry) Backend(id string) (Backend, bool) {
	id = strings.TrimSpace(id)
	for _, b := range r.backends {
		if b.ID == id {
			return b, true
		}
	}
	return Backend{}, false
}

func (r Registry) DefaultBackend() Backend {
	for _, b := range r.backends {
		if b.Default && b.Enabled && b.Health != BackendHealthInvalid {
			return b
		}
	}
	for _, b := range r.backends {
		if b.Enabled && b.Health == BackendHealthAvailable {
			return b
		}
	}
	return Backend{}
}

func builtInBackend(cfg config.CodegraphConfig) Backend {
	health := BackendHealthDisabled
	if cfg.Enabled {
		health = BackendHealthAvailable
	}
	return Backend{
		ID:             BuiltInBackendID,
		Name:           "CodeGraph",
		Server:         "codegraph",
		Kind:           "builtin",
		Default:        true,
		BuiltIn:        true,
		Enabled:        cfg.Enabled,
		Health:         health,
		IndexFreshness: "unknown",
		ToolCount:      len(ReadOnlyToolNames()),
		Capabilities: []BackendCapability{
			BackendCapabilitySymbolSearch,
			BackendCapabilityContextPack,
			BackendCapabilityGraphTrace,
			BackendCapabilityHealth,
		},
		Risks:       []loop.Capability{loop.CapabilityRead, loop.CapabilityProcess},
		ToolMapping: builtInToolMapping(),
	}
}

func fastContextStyleBackend(cfg config.CodegraphConfig) Backend {
	health := BackendHealthDisabled
	if cfg.Enabled {
		health = BackendHealthAvailable
	}
	return Backend{
		ID:             FastContextStyleBackendID,
		Name:           "FastContext-style explorer",
		Server:         "codegraph",
		Kind:           "builtin_fastcontext_style",
		Default:        false,
		BuiltIn:        true,
		Enabled:        cfg.Enabled,
		Health:         health,
		IndexFreshness: "unknown",
		ToolCount:      len(ReadOnlyToolNames()),
		Capabilities: []BackendCapability{
			BackendCapabilitySymbolSearch,
			BackendCapabilitySemanticSearch,
			BackendCapabilityContextPack,
			BackendCapabilityGraphTrace,
			BackendCapabilityHealth,
		},
		Risks:       []loop.Capability{loop.CapabilityRead, loop.CapabilityProcess},
		ToolMapping: builtInToolMapping(),
	}
}

func externalBackend(cfg config.CodegraphBackendConfig, runtime RuntimeSnapshot) Backend {
	id := strings.TrimSpace(cfg.Name)
	if id == "" {
		id = strings.TrimSpace(cfg.Server)
	}
	serverName := strings.TrimSpace(cfg.Server)
	if serverName == "" {
		serverName = id
	}
	b := Backend{
		ID:             id,
		Name:           strings.TrimSpace(cfg.Name),
		Server:         serverName,
		Kind:           "external_mcp",
		Enabled:        cfg.Enabled,
		Health:         BackendHealthDisabled,
		IndexFreshness: "unknown",
		Capabilities:   normalizeBackendCapabilities(cfg.Capabilities),
		Risks:          normalizeRisks(cfg.Risks),
		ToolMapping:    cloneToolMapping(cfg.ToolMapping),
	}
	if b.Name == "" {
		b.Name = b.ID
	}
	if len(b.Capabilities) == 0 {
		b.Capabilities = []BackendCapability{BackendCapabilitySymbolSearch, BackendCapabilityContextPack, BackendCapabilityHealth}
	}
	if len(b.Risks) == 0 {
		b.Risks = []loop.Capability{loop.CapabilityRead}
	}
	if !cfg.Enabled {
		return b
	}
	if err := validateRequiredToolMapping(b.ToolMapping); err != nil {
		b.Health = BackendHealthInvalid
		b.LastError = err.Error()
		return b
	}
	server, ok := runtime.Servers[serverName]
	if !ok || !server.Connected {
		b.Health = BackendHealthDegraded
		if ok && strings.TrimSpace(server.Error) != "" {
			b.LastError = server.Error
		} else {
			b.LastError = fmt.Sprintf("MCP server %q is not connected", serverName)
		}
		return b
	}
	if err := validateMappedToolsExist(b.ToolMapping, server.ToolNames); err != nil {
		b.Health = BackendHealthInvalid
		b.LastError = err.Error()
		return b
	}
	b.Health = BackendHealthAvailable
	b.ToolCount = server.ToolCount
	if b.ToolCount == 0 {
		b.ToolCount = len(server.ToolNames)
	}
	return b
}

func builtInToolMapping() map[string]string {
	return map[string]string{
		"callees": "mcp__codegraph__callees",
		"callers": "mcp__codegraph__callers",
		"context": "mcp__codegraph__context",
		"explore": "mcp__codegraph__explore",
		"files":   "mcp__codegraph__files",
		"impact":  "mcp__codegraph__impact",
		"node":    "mcp__codegraph__node",
		"search":  "mcp__codegraph__search",
		"status":  "mcp__codegraph__status",
		"trace":   "mcp__codegraph__trace",
	}
}

func validateRequiredToolMapping(mapping map[string]string) error {
	for _, key := range []string{"context", "search"} {
		if strings.TrimSpace(mapping[key]) == "" {
			return fmt.Errorf("missing required tool mapping %q", key)
		}
	}
	return nil
}

func validateMappedToolsExist(mapping map[string]string, toolNames []string) error {
	if len(toolNames) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, name := range toolNames {
		seen[strings.TrimSpace(name)] = true
	}
	for key, mapped := range mapping {
		mapped = strings.TrimSpace(mapped)
		if mapped != "" && !seen[mapped] {
			return fmt.Errorf("tool mapping %q points to unavailable tool %q", key, mapped)
		}
	}
	return nil
}

func cloneToolMapping(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func normalizeBackendCapabilities(items []string) []BackendCapability {
	out := []BackendCapability{}
	seen := map[BackendCapability]bool{}
	for _, raw := range items {
		cap := BackendCapability(strings.ToLower(strings.TrimSpace(raw)))
		switch cap {
		case BackendCapabilitySymbolSearch, BackendCapabilitySemanticSearch, BackendCapabilityContextPack, BackendCapabilityGraphTrace, BackendCapabilityEditRefactor, BackendCapabilityHealth, BackendCapabilityDenseVector, BackendCapabilitySparseVector, BackendCapabilityFullTextSearch, BackendCapabilityHybridSearch, BackendCapabilityWAL:
			if !seen[cap] {
				out = append(out, cap)
				seen[cap] = true
			}
		}
	}
	return out
}

func normalizeRisks(items []string) []loop.Capability {
	out := []loop.Capability{}
	seen := map[loop.Capability]bool{}
	for _, raw := range items {
		cap := loop.Capability(strings.ToLower(strings.TrimSpace(raw)))
		switch cap {
		case loop.CapabilityRead, loop.CapabilityWrite, loop.CapabilityNetwork, loop.CapabilityGit, loop.CapabilityCredential, loop.CapabilityProcess:
			if !seen[cap] {
				out = append(out, cap)
				seen[cap] = true
			}
		}
	}
	return out
}

func SortedBackendCapabilities(items []BackendCapability) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	sort.Strings(out)
	return out
}

func SortedRisks(items []loop.Capability) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	sort.Strings(out)
	return out
}
