package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/codegraph"
)

func TestRunDispatchesHyperGraphRAGStatus(t *testing.T) {
	isolateCLIConfigHome(t)

	out := captureStdout(t, func() {
		if rc := Run([]string{"hypergraphrag", "status"}, "test-version"); rc != 0 {
			t.Fatalf("Run hypergraphrag status rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "hypergraphrag: no configured backends") {
		t.Fatalf("hypergraphrag status output = %q, want no-backends message", out)
	}
}

func TestHyperGraphRAGStatusShowsConfiguredBackend(t *testing.T) {
	isolateCLIConfigHome(t)
	if err := os.WriteFile(filepath.Join(mustGetwd(t), "maddog.toml"), []byte(`
[[code_intelligence.backends]]
name = "project-hypergraph"
kind = "hypergraphrag"
command = "maddog-hypergraphrag"
args = ["--workdir", ".maddog/hypergraph"]
enabled = true

[code_intelligence.backends.env]
OPENAI_API_KEY = "${OPENAI_API_KEY}"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := hyperGraphRAGCommand([]string{"status"}); rc != 0 {
			t.Fatalf("hyperGraphRAGCommand status rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		"hypergraphrag backends: 1",
		"project-hypergraph",
		"status:        degraded",
		"command:       maddog-hypergraphrag",
		"args:          --workdir .maddog/hypergraph",
		"env_keys:      OPENAI_API_KEY",
		"capabilities:  semantic_search, context_pack, health",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("hypergraphrag status output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "${OPENAI_API_KEY}") {
		t.Fatalf("hypergraphrag status should not print env values:\n%s", out)
	}
}

func TestHyperGraphRAGStatusShowsInvalidBackend(t *testing.T) {
	isolateCLIConfigHome(t)
	if err := os.WriteFile(filepath.Join(mustGetwd(t), "maddog.toml"), []byte(`
[[code_intelligence.backends]]
name = "broken-hypergraph"
kind = "hypergraphrag"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := hyperGraphRAGCommand([]string{}); rc != 0 {
			t.Fatalf("hyperGraphRAGCommand default status rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		"broken-hypergraph",
		"status:        invalid",
		"error:         missing HyperGraphRAG command",
		"command:       (missing)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("invalid hypergraphrag status output missing %q:\n%s", want, out)
		}
	}
}

func TestHyperGraphRAGHealthIndexQuerySubcommandsRunSidecar(t *testing.T) {
	isolateCLIConfigHome(t)
	if err := os.WriteFile(filepath.Join(mustGetwd(t), "maddog.toml"), []byte(`
[[code_intelligence.backends]]
name = "project-hypergraph"
kind = "hypergraphrag"
command = "`+strings.ReplaceAll(os.Args[0], `\`, `\\`)+`"
args = ["-test.run=TestHyperGraphRAGCLIHelperProcess", "--"]
enabled = true

[code_intelligence.backends.env]
GO_WANT_HYPERGRAPHRAG_CLI_HELPER_PROCESS = "1"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	health := captureStdout(t, func() {
		if rc := hyperGraphRAGCommand([]string{"health", "--backend", "project-hypergraph"}); rc != 0 {
			t.Fatalf("health rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(health, `"status":"ready"`) && !strings.Contains(health, `"status": "ready"`) {
		t.Fatalf("health output = %s", health)
	}

	index := captureStdout(t, func() {
		if rc := hyperGraphRAGCommand([]string{"index", "--backend", "project-hypergraph", "--root", mustGetwd(t)}); rc != 0 {
			t.Fatalf("index rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(index, `"indexed":true`) && !strings.Contains(index, `"indexed": true`) {
		t.Fatalf("index output = %s", index)
	}

	query := captureStdout(t, func() {
		if rc := hyperGraphRAGCommand([]string{"query", "--backend", "project-hypergraph", "--capability", codegraph.BenchmarkCapabilitySemanticSearch, "--query", "advisor frontier routing"}); rc != 0 {
			t.Fatalf("query rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(query, `"advisor frontier routing"`) {
		t.Fatalf("query output = %s", query)
	}
}

func TestHyperGraphRAGCLIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HYPERGRAPHRAG_CLI_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			args = args[i+1:]
			break
		}
	}
	enc := json.NewEncoder(os.Stdout)
	switch args[0] {
	case "health":
		_ = enc.Encode(map[string]string{"status": codegraph.BenchmarkHealthReady})
	case "index":
		_ = enc.Encode(map[string]bool{"indexed": true})
	case "query":
		_ = enc.Encode(map[string]any{"results": []codegraph.BenchmarkResult{{
			ID:      "docs/cc/maddog-fusion--3949/tech.md",
			Title:   "advisor frontier routing",
			Content: "advisor frontier routing evidence",
		}}})
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
