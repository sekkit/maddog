package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
