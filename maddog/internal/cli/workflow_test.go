package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDispatchesWorkflowTemplateList(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := Run([]string{"workflows", "list"}, "test-version"); rc != 0 {
			t.Fatalf("Run workflows list rc = %d, want 0", rc)
		}
	})

	for _, want := range []string{"coding-task", "review-task", "skill-improvement"} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow list missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "default: coding-task") {
		t.Fatalf("workflow list should identify launch default:\n%s", out)
	}
}

func TestWorkflowTemplateListJSONUsesProjectOverrides(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".maddog", "loops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := `{
  "schemaVersion": "v1",
  "id": "coding-task",
  "name": "Project Coding",
  "goal": "Project-specific coding workflow",
  "risk": "high",
  "phases": [{"id":"plan","name":"Plan","goal":"Plan safely"}],
  "providerRoles": ["default","frontier"],
  "budget": {"frontierTokens": 2000, "totalTokens": 4000},
  "readinessGates": ["provider_configured"],
  "humanGates": ["git_push"],
  "makerChecker": {"mode":"review_only"},
  "requiredCapabilities": ["read","write"],
  "statePolicy": "project_override",
  "maxIterations": 4
}`
	if err := os.WriteFile(filepath.Join(dir, "coding-task.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"workflows", "list", "--json", "--dir", root}, "test-version"); rc != 0 {
			t.Fatalf("Run workflows list --json rc = %d, want 0", rc)
		}
	})

	var payload []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Source string `json:"source"`
		Hash   string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("workflow list output is not JSON: %v\n%s", err, out)
	}
	var coding *struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Source string `json:"source"`
		Hash   string `json:"hash"`
	}
	for i := range payload {
		if payload[i].ID == "coding-task" {
			coding = &payload[i]
			break
		}
	}
	if coding == nil || coding.Name != "Project Coding" || coding.Source != "project" || coding.Hash == "" {
		t.Fatalf("project override not reflected in JSON: %+v", payload)
	}
}

func TestWorkflowTemplateShowSelectsCodingTask(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := Run([]string{"workflows", "show", "coding-task"}, "test-version"); rc != 0 {
			t.Fatalf("Run workflows show rc = %d, want 0", rc)
		}
	})

	if !strings.Contains(out, "coding-task") || !strings.Contains(out, "provider roles:") || !strings.Contains(out, "human gates:") {
		t.Fatalf("workflow show did not expose launch metadata:\n%s", out)
	}
}
