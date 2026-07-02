package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"maddog/internal/codegraph"
	"maddog/internal/config"
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
	case "help", "-h", "--help":
		hyperGraphRAGUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown hypergraphrag subcommand %q\n\n", sub)
		hyperGraphRAGUsage()
		return 2
	}
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
  maddog hypergraphrag status   show configured sidecar backends without launching them

Configure HyperGraphRAG as an optional code-intelligence backend:

  [[code_intelligence.backends]]
  name = "project-hypergraph"
  kind = "hypergraphrag"
  command = "maddog-hypergraphrag"
  args = ["--workdir", ".maddog/hypergraph"]
  enabled = true

`)
}
