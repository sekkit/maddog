package hypergraphrag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"maddog/internal/codegraph"
)

func TestBenchmarkBackendDrivesSidecarIndexAndQuery(t *testing.T) {
	backend := NewBenchmarkBackend(helperSidecarConfig(t))

	report := codegraph.RunBenchmark(context.Background(), codegraph.BenchmarkOptions{
		Root:     t.TempDir(),
		Backends: []codegraph.BenchmarkBackend{backend},
		Cases: []codegraph.BenchmarkCase{{
			Name:        "semantic architecture search",
			Query:       "advisor frontier routing",
			Capability:  codegraph.BenchmarkCapabilitySemanticSearch,
			ExpectedIDs: []string{"docs/cc/maddog-fusion--3949/tech.md"},
			TopK:        3,
		}},
	})

	got := report.Backends[0]
	if got.ID != DefaultBackendID || got.Name != DefaultBackendName {
		t.Fatalf("backend identity = %+v, want HyperGraphRAG defaults", got)
	}
	if got.Health != codegraph.BenchmarkHealthReady || got.Failures != 0 {
		t.Fatalf("backend health/failures = %+v, want ready with no failures", got)
	}
	query := got.Queries[0]
	if query.Status != codegraph.BenchmarkQueryOK || query.ResultCount != 1 || query.RelevanceScore != 1 {
		t.Fatalf("query report = %+v, want one relevant result", query)
	}
}

func TestBenchmarkBackendSkipsUnsupportedSymbolSearch(t *testing.T) {
	backend := NewBenchmarkBackend(helperSidecarConfig(t))
	report := codegraph.RunBenchmark(context.Background(), codegraph.BenchmarkOptions{
		Root:     t.TempDir(),
		Backends: []codegraph.BenchmarkBackend{backend},
		Cases: []codegraph.BenchmarkCase{{
			Name:       "symbol",
			Query:      "RunBenchmark",
			Capability: codegraph.BenchmarkCapabilitySymbolSearch,
		}},
	})
	if got := report.Backends[0].Queries[0].Status; got != codegraph.BenchmarkQueryUnsupported {
		t.Fatalf("symbol status = %q, want unsupported", got)
	}
}

func TestBenchmarkBackendExpandsConfiguredEnv(t *testing.T) {
	t.Setenv("MADDOG_HYPERGRAPHRAG_TEST_KEY", "expanded-secret")
	cfg := helperSidecarConfig(t)
	cfg.Env["GO_WANT_HYPERGRAPHRAG_ENV"] = "${MADDOG_HYPERGRAPHRAG_TEST_KEY}"
	backend := NewBenchmarkBackend(cfg)

	if _, err := backend.health(context.Background()); err != nil {
		t.Fatalf("health with expanded env: %v", err)
	}
}

func helperSidecarConfig(t *testing.T) SidecarConfig {
	t.Helper()
	return SidecarConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHyperGraphRAGSidecarHelper", "--"},
		Env: map[string]string{
			"GO_WANT_HYPERGRAPHRAG_HELPER_PROCESS": "1",
		},
	}
}

func TestHyperGraphRAGSidecarHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HYPERGRAPHRAG_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	switch args[0] {
	case "health":
		if want := os.Getenv("GO_WANT_HYPERGRAPHRAG_ENV"); want != "" && want != "expanded-secret" {
			fmt.Fprintln(os.Stderr, "GO_WANT_HYPERGRAPHRAG_ENV was not expanded")
			os.Exit(3)
		}
		_ = enc.Encode(HealthResponse{Status: codegraph.BenchmarkHealthReady})
	case "index":
		_ = enc.Encode(map[string]any{"indexed": true})
	case "query":
		if !strings.Contains(strings.Join(args, " "), "advisor frontier routing") {
			_ = enc.Encode(QueryResponse{})
			os.Exit(0)
		}
		_ = enc.Encode(QueryResponse{Results: []codegraph.BenchmarkResult{{
			ID:      "docs/cc/maddog-fusion--3949/tech.md",
			Title:   "advisor frontier routing",
			Content: "advisor and frontier routing design evidence",
		}}})
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
