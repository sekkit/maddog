package codegraph

import (
	"fmt"
	"sort"
	"strings"

	"maddog/internal/config"
)

const (
	BuiltInBackendID = "codegraph"

	BackendKindBuiltIn       = "builtin"
	BackendKindMCP           = "mcp"
	BackendKindHyperGraphRAG = "hypergraphrag"

	BackendHealthReady    = "ready"
	BackendHealthUnknown  = "unknown"
	BackendHealthDegraded = "degraded"
	BackendHealthDisabled = "disabled"
	BackendHealthInvalid  = "invalid"
)

type BackendCapabilities struct {
	SymbolSearch   bool
	SemanticSearch bool
	ContextPack    bool
	GraphTrace     bool
	EditRefactor   bool
	Health         bool
}

type BackendHealth struct {
	Status string
	Error  string
}

type Backend struct {
	ID           string
	Name         string
	Kind         string
	ServerName   string
	Enabled      bool
	Command      string
	Args         []string
	IndexMode    string
	Env          map[string]string
	Capabilities BackendCapabilities
	ToolMapping  map[string]string
	Health       BackendHealth
}

type BackendRegistry struct {
	backends map[string]Backend
	invalid  []Backend
	order    []string
}

func NewBackendRegistry(cfg *config.Config) BackendRegistry {
	reg := BackendRegistry{backends: map[string]Backend{}}
	if cfg == nil {
		return reg
	}
	reg.addBuiltIn(cfg.Codegraph)
	for _, entry := range cfg.CodeIntelligence.Backends {
		reg.addExternal(entry)
	}
	return reg
}

func (r BackendRegistry) Backends() []Backend {
	out := make([]Backend, 0, len(r.order))
	for _, id := range r.order {
		if b, ok := r.backends[id]; ok {
			out = append(out, cloneBackend(b))
		}
	}
	return out
}

func (r BackendRegistry) Backend(id string) (Backend, bool) {
	b, ok := r.backends[strings.TrimSpace(id)]
	if !ok {
		return Backend{}, false
	}
	return cloneBackend(b), true
}

func (r BackendRegistry) InvalidBackends() []Backend {
	out := make([]Backend, 0, len(r.invalid))
	for _, b := range r.invalid {
		out = append(out, cloneBackend(b))
	}
	return out
}

func (r *BackendRegistry) addBuiltIn(cfg config.CodegraphConfig) {
	mapping := map[string]string{
		"symbol_search": "mcp__codegraph__search",
		"context_pack":  "mcp__codegraph__context",
		"graph_trace":   "mcp__codegraph__trace",
		"health":        "mcp__codegraph__status",
	}
	status := BackendHealthReady
	if !cfg.Enabled {
		status = BackendHealthDisabled
	}
	r.register(Backend{
		ID:           BuiltInBackendID,
		Name:         "CodeGraph",
		Kind:         BackendKindBuiltIn,
		ServerName:   "codegraph",
		Enabled:      cfg.Enabled,
		Capabilities: capabilitiesFromTools(mapping),
		ToolMapping:  mapping,
		Health:       BackendHealth{Status: status},
	})
}

func (r *BackendRegistry) addExternal(entry config.CodeIntelligenceBackendConfig) {
	id := strings.TrimSpace(entry.Name)
	if id == "" {
		id = strings.TrimSpace(entry.Server)
	}
	if id == "" {
		id = strings.TrimSpace(entry.Command)
	}
	backend := Backend{
		ID:          id,
		Name:        id,
		Kind:        normalizeBackendKind(entry.Kind),
		ServerName:  strings.TrimSpace(entry.Server),
		Enabled:     entry.IsEnabled(),
		Command:     strings.TrimSpace(entry.Command),
		Args:        cloneStringSlice(entry.Args),
		IndexMode:   strings.TrimSpace(entry.IndexMode),
		Env:         cloneStringMap(entry.Env),
		ToolMapping: cloneStringMap(entry.Tools),
	}
	if backend.Kind == "" {
		backend.Kind = BackendKindMCP
	}
	if backend.Kind == BackendKindMCP && backend.ServerName == "" {
		backend.ServerName = backend.ID
	}
	backend.Capabilities = capabilitiesFromTools(backend.ToolMapping)
	if backend.Kind == BackendKindHyperGraphRAG && len(backend.ToolMapping) == 0 {
		backend.Capabilities = BackendCapabilities{SemanticSearch: true, ContextPack: true, Health: true}
	}
	if _, exists := r.backends[backend.ID]; exists {
		backend.Health = BackendHealth{Status: BackendHealthInvalid, Error: fmt.Sprintf("backend name %q is reserved or already registered", backend.ID)}
		r.invalid = append(r.invalid, backend)
		return
	}
	if !backend.Enabled {
		backend.Health = BackendHealth{Status: BackendHealthDisabled}
		r.register(backend)
		return
	}
	if err := validateExternalBackend(backend); err != nil {
		backend.Health = BackendHealth{Status: BackendHealthInvalid, Error: err.Error()}
		r.invalid = append(r.invalid, backend)
		return
	}
	backend.Health = BackendHealth{Status: BackendHealthDegraded}
	r.register(backend)
}

func (r *BackendRegistry) register(b Backend) {
	if b.ID == "" {
		return
	}
	if _, exists := r.backends[b.ID]; !exists {
		r.order = append(r.order, b.ID)
	}
	r.backends[b.ID] = cloneBackend(b)
}

func normalizeBackendKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", BackendKindMCP:
		return BackendKindMCP
	case BackendKindBuiltIn:
		return BackendKindBuiltIn
	case BackendKindHyperGraphRAG, "hypergraph-rag", "hypergraph_rag":
		return BackendKindHyperGraphRAG
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func validateExternalBackend(b Backend) error {
	if b.ID == "" {
		return fmt.Errorf("missing backend name")
	}
	switch b.Kind {
	case BackendKindMCP:
		return validateMCPBackend(b)
	case BackendKindHyperGraphRAG:
		return validateHyperGraphRAGBackend(b)
	default:
		return fmt.Errorf("unsupported backend kind %q", b.Kind)
	}
}

func validateMCPBackend(b Backend) error {
	if b.ServerName == "" {
		return fmt.Errorf("missing MCP server")
	}
	if len(b.ToolMapping) == 0 {
		return fmt.Errorf("missing tool mapping")
	}
	for capability, toolName := range b.ToolMapping {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			return fmt.Errorf("tool mapping %q is empty", capability)
		}
		if !strings.HasPrefix(toolName, "mcp__"+b.ServerName+"__") {
			return fmt.Errorf("tool mapping %q uses %q, want mcp__%s__* tool", capability, toolName, b.ServerName)
		}
	}
	if !b.Capabilities.SymbolSearch && !b.Capabilities.SemanticSearch && !b.Capabilities.ContextPack && !b.Capabilities.GraphTrace {
		return fmt.Errorf("tool mapping exposes no code intelligence capability")
	}
	return nil
}

func validateHyperGraphRAGBackend(b Backend) error {
	if b.Command == "" {
		return fmt.Errorf("missing HyperGraphRAG command")
	}
	if !b.Capabilities.SemanticSearch && !b.Capabilities.ContextPack {
		return fmt.Errorf("HyperGraphRAG backend exposes no semantic/context capability")
	}
	return nil
}

func capabilitiesFromTools(tools map[string]string) BackendCapabilities {
	_, symbol := tools["symbol_search"]
	_, semantic := tools["semantic_search"]
	_, context := tools["context_pack"]
	_, graphTrace := tools["graph_trace"]
	_, edit := tools["edit_refactor"]
	_, health := tools["health"]
	return BackendCapabilities{
		SymbolSearch:   symbol,
		SemanticSearch: semantic,
		ContextPack:    context,
		GraphTrace:     graphTrace,
		EditRefactor:   edit,
		Health:         health,
	}
}

func cloneBackend(b Backend) Backend {
	b.Args = cloneStringSlice(b.Args)
	b.Env = cloneStringMap(b.Env)
	b.ToolMapping = cloneStringMap(b.ToolMapping)
	return b
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = in[k]
	}
	return out
}
