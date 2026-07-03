package codegraph

import "maddog/internal/config"

type BackendPreset struct {
	ID           string
	Name         string
	Status       string
	Notes        string
	Backend      config.CodeIntelligenceBackendConfig
	ResearchOnly bool
}

func KnownBackendPresets() []BackendPreset {
	enabled := true
	return []BackendPreset{
		{
			ID:     "serena",
			Name:   "Serena",
			Status: "supported",
			Notes:  "MCP symbol backend; configure the serena MCP server separately.",
			Backend: config.CodeIntelligenceBackendConfig{
				Name:    "serena",
				Kind:    BackendKindMCP,
				Server:  "serena",
				Enabled: &enabled,
				Tools: map[string]string{
					"symbol_search": "mcp__serena__find_symbol",
					"context_pack":  "mcp__serena__get_symbols_overview",
				},
			},
		},
		{
			ID:     "claude-context",
			Name:   "Claude Context",
			Status: "supported",
			Notes:  "MCP context backend; configure the claude-context MCP server separately.",
			Backend: config.CodeIntelligenceBackendConfig{
				Name:    "claude-context",
				Kind:    BackendKindMCP,
				Server:  "claude-context",
				Enabled: &enabled,
				Tools: map[string]string{
					"semantic_search": "mcp__claude-context__search_code",
					"context_pack":    "mcp__claude-context__get_context",
				},
			},
		},
		{
			ID:     "codebase-memory",
			Name:   "Codebase Memory",
			Status: "supported",
			Notes:  "MCP memory backend; configure the codebase-memory MCP server separately.",
			Backend: config.CodeIntelligenceBackendConfig{
				Name:    "codebase-memory",
				Kind:    BackendKindMCP,
				Server:  "codebase-memory",
				Enabled: &enabled,
				Tools: map[string]string{
					"semantic_search": "mcp__codebase-memory__search",
					"context_pack":    "mcp__codebase-memory__context",
				},
			},
		},
		{
			ID:           "zvec",
			Name:         "zvec",
			Status:       "research_only",
			Notes:        "zvec is a vector library, not a Maddog MCP/backend adapter.",
			ResearchOnly: true,
		},
	}
}
