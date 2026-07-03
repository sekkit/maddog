package hypergraphrag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestBenchmarkInfoHealthCheckUsesConfiguredTimeout(t *testing.T) {
	cfg := helperSidecarConfig(t)
	cfg.Timeout = 20 * time.Millisecond
	cfg.Env["GO_WANT_HYPERGRAPHRAG_SLEEP_MS"] = "250"
	backend := NewBenchmarkBackend(cfg)

	start := time.Now()
	info := backend.BenchmarkInfo()
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("BenchmarkInfo took %s, want bounded timeout", elapsed)
	}
	if info.Health != codegraph.BackendHealthDegraded {
		t.Fatalf("health = %q, want degraded on timeout", info.Health)
	}
}

func TestBenchmarkBackendQueryOnlyModeSkipsIndexCommands(t *testing.T) {
	cfg := helperSidecarConfig(t)
	cfg.IndexMode = IndexModeQueryOnly
	cfg.Env["GO_WANT_HYPERGRAPHRAG_FAIL_INDEX"] = "1"
	backend := NewBenchmarkBackend(cfg)

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
	if got.Failures != 0 || got.Health != codegraph.BenchmarkHealthReady {
		t.Fatalf("query-only benchmark = %+v, want no index failure", got)
	}
	if got.IndexBuildMillis != 0 || got.IncrementalUpdateMillis != 0 {
		t.Fatalf("query-only index timings = build %d update %d, want zero", got.IndexBuildMillis, got.IncrementalUpdateMillis)
	}
	if got.Queries[0].Status != codegraph.BenchmarkQueryOK {
		t.Fatalf("query-only query = %+v, want ok", got.Queries[0])
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
	if sleepMS := os.Getenv("GO_WANT_HYPERGRAPHRAG_SLEEP_MS"); sleepMS != "" {
		ms, err := strconv.Atoi(sleepMS)
		if err != nil {
			os.Exit(4)
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	switch args[0] {
	case "health":
		if want := os.Getenv("GO_WANT_HYPERGRAPHRAG_ENV"); want != "" && want != "expanded-secret" {
			fmt.Fprintln(os.Stderr, "GO_WANT_HYPERGRAPHRAG_ENV was not expanded")
			os.Exit(3)
		}
		_ = enc.Encode(HealthResponse{Status: codegraph.BenchmarkHealthReady})
	case "index":
		if os.Getenv("GO_WANT_HYPERGRAPHRAG_FAIL_INDEX") == "1" {
			fmt.Fprintln(os.Stderr, "index should not be called")
			os.Exit(5)
		}
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
