package hypergraphrag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"maddog/internal/codegraph"
)

const (
	DefaultBackendID   = "hypergraphrag"
	DefaultBackendName = "HyperGraphRAG"
)

type SidecarConfig struct {
	ID      string
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

type HealthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type QueryResponse struct {
	Results []codegraph.BenchmarkResult `json:"results"`
}

type BenchmarkBackend struct {
	cfg SidecarConfig
}

func NewBenchmarkBackend(cfg SidecarConfig) *BenchmarkBackend {
	return &BenchmarkBackend{cfg: cfg}
}

func (b *BenchmarkBackend) BenchmarkInfo() codegraph.BenchmarkBackendInfo {
	health := codegraph.BackendHealthDegraded
	if err := b.validate(); err != nil {
		health = codegraph.BackendHealthInvalid
	} else if res, err := b.health(context.Background()); err == nil && strings.TrimSpace(res.Status) != "" {
		health = strings.TrimSpace(res.Status)
	}
	return codegraph.BenchmarkBackendInfo{
		ID:   valueOr(b.cfg.ID, DefaultBackendID),
		Name: valueOr(b.cfg.Name, DefaultBackendName),
		Capabilities: codegraph.BackendCapabilities{
			SemanticSearch: true,
			ContextPack:    true,
			Health:         true,
		},
		Health: health,
	}
}

func (b *BenchmarkBackend) BuildIndex(ctx context.Context, root string) error {
	if err := b.validate(); err != nil {
		return err
	}
	_, err := b.run(ctx, "index", "--root", root, "--json")
	return err
}

func (b *BenchmarkBackend) UpdateIndex(ctx context.Context, root string) error {
	return b.BuildIndex(ctx, root)
}

func (b *BenchmarkBackend) Query(ctx context.Context, query codegraph.BenchmarkQuery) ([]codegraph.BenchmarkResult, error) {
	if query.Capability != codegraph.BenchmarkCapabilitySemanticSearch && query.Capability != "context_pack" {
		return nil, nil
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	args := []string{"query", "--capability", query.Capability, "--query", query.Text, "--json"}
	if query.TopK > 0 {
		args = append(args, "--top-k", strconv.Itoa(query.TopK))
	}
	if query.BudgetTokens > 0 {
		args = append(args, "--budget-tokens", strconv.Itoa(query.BudgetTokens))
	}
	raw, err := b.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var res QueryResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("decode HyperGraphRAG query response: %w", err)
	}
	return res.Results, nil
}

func (b *BenchmarkBackend) health(ctx context.Context) (HealthResponse, error) {
	if err := b.validate(); err != nil {
		return HealthResponse{}, err
	}
	raw, err := b.run(ctx, "health", "--json")
	if err != nil {
		return HealthResponse{}, err
	}
	var res HealthResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return HealthResponse{}, fmt.Errorf("decode HyperGraphRAG health response: %w", err)
	}
	return res, nil
}

func (b *BenchmarkBackend) validate() error {
	if strings.TrimSpace(b.cfg.Command) == "" {
		return fmt.Errorf("HyperGraphRAG command is required")
	}
	return nil
}

func (b *BenchmarkBackend) run(ctx context.Context, actionArgs ...string) ([]byte, error) {
	args := append([]string{}, b.cfg.Args...)
	args = append(args, actionArgs...)
	cmd := exec.CommandContext(ctx, b.cfg.Command, args...)
	cmd.Env = os.Environ()
	for k, v := range b.cfg.Env {
		if strings.TrimSpace(k) == "" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+os.ExpandEnv(v))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("HyperGraphRAG sidecar %s: %s", strings.Join(actionArgs, " "), msg)
	}
	return out, nil
}

func valueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
