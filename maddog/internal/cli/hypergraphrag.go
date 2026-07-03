package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"maddog/internal/codegraph"
	"maddog/internal/config"
	"maddog/internal/hypergraphrag"
)

// hyperGraphRAGCommand backs `maddog hypergraphrag` for inspecting configured
// HyperGraphRAG sidecar backends. It is intentionally read-only: health checks
// that launch Python live in the benchmark path, not status output.
func hyperGraphRAGCommand(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status", "":
		return hyperGraphRAGStatus()
	case "health":
		return hyperGraphRAGHealth(args[1:])
	case "index":
		return hyperGraphRAGIndex(args[1:])
	case "query":
		return hyperGraphRAGQuery(args[1:])
	case "help", "-h", "--help":
		hyperGraphRAGUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown hypergraphrag subcommand %q\n\n", sub)
		hyperGraphRAGUsage()
		return 2
	}
}

func hyperGraphRAGHealth(args []string) int {
	fs := flag.NewFlagSet("hypergraphrag health", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	backendID := fs.String("backend", "", "HyperGraphRAG backend id")
	timeout := fs.Duration("timeout", hypergraphrag.DefaultSidecarTimeout, "sidecar command timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	backend, ok := resolveHyperGraphRAGBackend(*backendID)
	if !ok {
		return 1
	}
	res, err := hypergraphrag.Health(context.Background(), sidecarConfigFromBackendWithTimeout(backend, *timeout))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(res)
}

func hyperGraphRAGIndex(args []string) int {
	fs := flag.NewFlagSet("hypergraphrag index", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	backendID := fs.String("backend", "", "HyperGraphRAG backend id")
	root := fs.String("root", ".", "repository root to index")
	timeout := fs.Duration("timeout", hypergraphrag.DefaultSidecarTimeout, "sidecar command timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	backend, ok := resolveHyperGraphRAGBackend(*backendID)
	if !ok {
		return 1
	}
	if err := hypergraphrag.Index(context.Background(), sidecarConfigFromBackendWithTimeout(backend, *timeout), *root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(map[string]bool{"indexed": true})
}

func hyperGraphRAGQuery(args []string) int {
	fs := flag.NewFlagSet("hypergraphrag query", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	backendID := fs.String("backend", "", "HyperGraphRAG backend id")
	capability := fs.String("capability", codegraph.BenchmarkCapabilitySemanticSearch, "query capability")
	text := fs.String("query", "", "query text")
	topK := fs.Int("top-k", 0, "maximum results")
	budgetTokens := fs.Int("budget-tokens", 0, "context budget")
	timeout := fs.Duration("timeout", hypergraphrag.DefaultSidecarTimeout, "sidecar command timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*text) == "" {
		fmt.Fprintln(os.Stderr, "query text is required")
		return 2
	}
	backend, ok := resolveHyperGraphRAGBackend(*backendID)
	if !ok {
		return 1
	}
	results, err := hypergraphrag.Query(context.Background(), sidecarConfigFromBackendWithTimeout(backend, *timeout), codegraph.BenchmarkQuery{
		Text:         *text,
		Capability:   *capability,
		TopK:         *topK,
		BudgetTokens: *budgetTokens,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printJSON(hypergraphrag.QueryResponse{Results: results})
}

func resolveHyperGraphRAGBackend(backendID string) (codegraph.Backend, bool) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return codegraph.Backend{}, false
	}
	backends := hyperGraphRAGBackends(codegraph.NewBackendRegistry(cfg))
	if len(backends) == 0 {
		fmt.Fprintln(os.Stderr, "no HyperGraphRAG backends configured")
		return codegraph.Backend{}, false
	}
	backendID = strings.TrimSpace(backendID)
	if backendID == "" {
		if len(backends) > 1 {
			fmt.Fprintln(os.Stderr, "multiple HyperGraphRAG backends configured; pass --backend")
			return codegraph.Backend{}, false
		}
		return backends[0], true
	}
	for _, backend := range backends {
		if backend.ID == backendID {
			if !backend.Enabled {
				fmt.Fprintf(os.Stderr, "HyperGraphRAG backend %q is disabled\n", backendID)
				return codegraph.Backend{}, false
			}
			if backend.Health.Status == codegraph.BackendHealthInvalid {
				fmt.Fprintf(os.Stderr, "HyperGraphRAG backend %q is invalid: %s\n", backendID, backend.Health.Error)
				return codegraph.Backend{}, false
			}
			return backend, true
		}
	}
	fmt.Fprintf(os.Stderr, "unknown HyperGraphRAG backend %q\n", backendID)
	return codegraph.Backend{}, false
}

func sidecarConfigFromBackend(backend codegraph.Backend) hypergraphrag.SidecarConfig {
	return sidecarConfigFromBackendWithTimeout(backend, 0)
}

func sidecarConfigFromBackendWithTimeout(backend codegraph.Backend, timeout time.Duration) hypergraphrag.SidecarConfig {
	return hypergraphrag.SidecarConfig{
		ID:      backend.ID,
		Name:    backend.Name,
		Command: backend.Command,
		Args:    backend.Args,
		Env:     backend.Env,
		Timeout: timeout,
	}
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func hyperGraphRAGStatus() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	reg := codegraph.NewBackendRegistry(cfg)
	backends := hyperGraphRAGBackends(reg)
	if len(backends) == 0 {
		fmt.Println("hypergraphrag: no configured backends")
		fmt.Println("add a [[code_intelligence.backends]] entry with kind = \"hypergraphrag\"")
		return 0
	}
	fmt.Printf("hypergraphrag backends: %d\n", len(backends))
	for i, backend := range backends {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s\n", backend.ID)
		fmt.Printf("%-14s %v\n", "enabled:", backend.Enabled)
		fmt.Printf("%-14s %s\n", "status:", valueOrCLI(backend.Health.Status, codegraph.BackendHealthUnknown))
		if backend.Health.Error != "" {
			fmt.Printf("%-14s %s\n", "error:", backend.Health.Error)
		}
		fmt.Printf("%-14s %s\n", "command:", valueOrCLI(backend.Command, "(missing)"))
		if len(backend.Args) > 0 {
			fmt.Printf("%-14s %s\n", "args:", strings.Join(backend.Args, " "))
		}
		if len(backend.Env) > 0 {
			fmt.Printf("%-14s %s\n", "env_keys:", strings.Join(sortedBackendKeys(backend.Env), ", "))
		}
		fmt.Printf("%-14s %s\n", "capabilities:", strings.Join(backendCapabilityNames(backend.Capabilities), ", "))
	}
	return 0
}

func hyperGraphRAGBackends(reg codegraph.BackendRegistry) []codegraph.Backend {
	var out []codegraph.Backend
	for _, backend := range reg.Backends() {
		if backend.Kind == codegraph.BackendKindHyperGraphRAG {
			out = append(out, backend)
		}
	}
	for _, backend := range reg.InvalidBackends() {
		if backend.Kind == codegraph.BackendKindHyperGraphRAG {
			out = append(out, backend)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func backendCapabilityNames(c codegraph.BackendCapabilities) []string {
	var names []string
	if c.SymbolSearch {
		names = append(names, "symbol_search")
	}
	if c.SemanticSearch {
		names = append(names, "semantic_search")
	}
	if c.ContextPack {
		names = append(names, "context_pack")
	}
	if c.GraphTrace {
		names = append(names, "graph_trace")
	}
	if c.EditRefactor {
		names = append(names, "edit_refactor")
	}
	if c.Health {
		names = append(names, "health")
	}
	if len(names) == 0 {
		return []string{"(none)"}
	}
	return names
}

func sortedBackendKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func valueOrCLI(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func hyperGraphRAGUsage() {
	fmt.Print(`maddog hypergraphrag — inspect HyperGraphRAG code-intelligence sidecars

Usage:
  maddog hypergraphrag status                         show configured sidecar backends without launching them
  maddog hypergraphrag health --backend <id> [--timeout 2m]           run the configured sidecar health check
  maddog hypergraphrag index --backend <id> --root . [--timeout 2m]   run sidecar indexing
  maddog hypergraphrag query --backend <id> --query q [--timeout 2m]  run a semantic/context query

Configure HyperGraphRAG as an optional code-intelligence backend:

  [[code_intelligence.backends]]
  name = "project-hypergraph"
  kind = "hypergraphrag"
  command = "maddog-hypergraphrag"
  args = ["--workdir", ".maddog/hypergraph"]
  enabled = true

`)
}
