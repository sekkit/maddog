package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"maddog/internal/tool"
)

type MappedMCPBenchmarkBackend struct {
	backend Backend
	reg     *tool.Registry
}

func NewMappedMCPBenchmarkBackend(backend Backend, reg *tool.Registry) *MappedMCPBenchmarkBackend {
	return &MappedMCPBenchmarkBackend{backend: cloneBackend(backend), reg: reg}
}

func (b *MappedMCPBenchmarkBackend) BenchmarkInfo() BenchmarkBackendInfo {
	health := BenchmarkHealthReady
	if b.reg == nil {
		health = BackendHealthDegraded
	} else {
		for capability := range b.backend.ToolMapping {
			if _, err := b.toolFor(capability); err != nil {
				health = BackendHealthDegraded
				break
			}
		}
	}
	return BenchmarkBackendInfo{
		ID:           b.backend.ID,
		Name:         nonEmpty(b.backend.Name, b.backend.ID),
		Capabilities: b.backend.Capabilities,
		Health:       health,
	}
}

func (b *MappedMCPBenchmarkBackend) BuildIndex(context.Context, string) error { return nil }

func (b *MappedMCPBenchmarkBackend) UpdateIndex(context.Context, string) error { return nil }

func (b *MappedMCPBenchmarkBackend) Query(ctx context.Context, query BenchmarkQuery) ([]BenchmarkResult, error) {
	tl, err := b.toolFor(query.Capability)
	if err != nil {
		return nil, err
	}
	raw, err := mappedBenchmarkArgs(tl.Schema(), query)
	if err != nil {
		return nil, err
	}
	out, err := tl.Execute(ctx, raw)
	if err != nil {
		return nil, err
	}
	return []BenchmarkResult{{
		ID:      strings.TrimSpace(b.backend.ToolMapping[query.Capability]),
		Title:   query.Text,
		Content: out,
	}}, nil
}

func mappedBenchmarkArgs(schema json.RawMessage, query BenchmarkQuery) (json.RawMessage, error) {
	fallback := map[string]any{
		"query":         query.Text,
		"symbol":        query.Text,
		"name":          query.Text,
		"top_k":         query.TopK,
		"max_results":   query.TopK,
		"budget_tokens": query.BudgetTokens,
	}
	var doc struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if len(schema) == 0 || json.Unmarshal(schema, &doc) != nil || len(doc.Properties) == 0 {
		return json.Marshal(fallback)
	}
	required := map[string]bool{}
	for _, key := range doc.Required {
		required[key] = true
	}
	out := map[string]any{}
	for key := range doc.Properties {
		value, ok := benchmarkArgCandidate(key, query, required[key])
		if !ok {
			if required[key] {
				return nil, fmt.Errorf("mapped MCP benchmark tool schema requires unsupported required argument %q", key)
			}
			continue
		}
		out[key] = value
	}
	for key := range required {
		if _, ok := doc.Properties[key]; !ok {
			return nil, fmt.Errorf("mapped MCP benchmark tool schema requires unsupported required argument %q", key)
		}
	}
	return json.Marshal(out)
}

func benchmarkArgCandidate(key string, query BenchmarkQuery, required bool) (any, bool) {
	switch key {
	case "query", "symbol", "name", "q", "text":
		if strings.TrimSpace(query.Text) == "" && !required {
			return nil, false
		}
		return query.Text, true
	case "capability":
		if strings.TrimSpace(query.Capability) == "" && !required {
			return nil, false
		}
		return query.Capability, true
	case "top_k", "max_results", "limit":
		if query.TopK <= 0 && !required {
			return nil, false
		}
		return query.TopK, true
	case "budget_tokens":
		if query.BudgetTokens <= 0 && !required {
			return nil, false
		}
		return query.BudgetTokens, true
	default:
		return nil, false
	}
}

func (b *MappedMCPBenchmarkBackend) toolFor(capability string) (tool.Tool, error) {
	if b.reg == nil {
		return nil, fmt.Errorf("MCP backend %q has no live tool registry", b.backend.ID)
	}
	toolName := strings.TrimSpace(b.backend.ToolMapping[capability])
	if toolName == "" {
		return nil, fmt.Errorf("MCP backend %q has no mapped tool for %s", b.backend.ID, capability)
	}
	tl, ok := b.reg.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("MCP backend %q mapped tool %q is not connected", b.backend.ID, toolName)
	}
	return tl, nil
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
