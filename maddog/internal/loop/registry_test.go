package loop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltInRegistryReturnsWorkflowTemplates(t *testing.T) {
	templates, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	if len(templates) != 3 {
		t.Fatalf("template count = %d, want 3", len(templates))
	}

	byID := map[string]LoopTemplateV1{}
	for _, tmpl := range templates {
		byID[tmpl.ID] = tmpl
		if tmpl.SchemaVersion != "v1" {
			t.Fatalf("%s schema = %q, want v1", tmpl.ID, tmpl.SchemaVersion)
		}
		if tmpl.Source == "" || tmpl.Hash == "" {
			t.Fatalf("%s missing source/hash: %+v", tmpl.ID, tmpl)
		}
	}

	coding, ok := byID["coding-task"]
	if !ok {
		t.Fatalf("coding-task template missing: %v", templates)
	}
	if coding.Name == "" || coding.Goal == "" || coding.Budget.FrontierTokens <= 0 || coding.MaxIterations <= 0 {
		t.Fatalf("coding-task incomplete: %+v", coding)
	}
	if !contains(coding.ProviderRoles, "frontier") || !contains(coding.ProviderRoles, "small") {
		t.Fatalf("coding-task provider roles = %v, want frontier and small", coding.ProviderRoles)
	}
	if !contains(coding.RequiredCapabilities, CapabilityRead) || !contains(coding.RequiredCapabilities, CapabilityWrite) || !contains(coding.RequiredCapabilities, CapabilityGit) {
		t.Fatalf("coding-task capabilities = %v, want read/write/git", coding.RequiredCapabilities)
	}
	if coding.MakerChecker.Mode != MakerCheckerReviewOnly {
		t.Fatalf("coding-task maker checker mode = %q, want %q", coding.MakerChecker.Mode, MakerCheckerReviewOnly)
	}
	if !contains(coding.Artifacts.TaskPacketFields, "request") || !contains(coding.Artifacts.TaskPacketFields, "acceptance_criteria") {
		t.Fatalf("coding-task task packet fields = %v, want request and acceptance_criteria", coding.Artifacts.TaskPacketFields)
	}
	if coding.Artifacts.BoundedFanOut.MaxParallel < 1 || coding.Artifacts.BoundedFanOut.RequiresHumanApproval {
		t.Fatalf("coding-task bounded fan-out should be usable and not require default approval: %+v", coding.Artifacts.BoundedFanOut)
	}
	if !contains(coding.Artifacts.DelegationArtifacts, "worker_summary") || !contains(coding.Artifacts.IntegrationChecklist, "run_focused_tests") {
		t.Fatalf("coding-task artifact review metadata incomplete: %+v", coding.Artifacts)
	}
	if !contains(coding.Artifacts.FinalVerificationArtifacts, "run_report") {
		t.Fatalf("coding-task final verification artifacts = %v, want run_report", coding.Artifacts.FinalVerificationArtifacts)
	}
	if !artifactMapsTo(coding.Artifacts.RunReportMapping, "final_verification", "report.finalStatus") {
		t.Fatalf("coding-task run report mapping missing final verification link: %+v", coding.Artifacts.RunReportMapping)
	}
	if !coding.RefinementStrategy.Enabled {
		t.Fatalf("coding-task must enable iterative refinement by default for v1: %+v", coding.RefinementStrategy)
	}
	if !contains(coding.RefinementStrategy.SearchModes, RefinementSearchBFSHypothesis) || !contains(coding.RefinementStrategy.SearchModes, RefinementSearchDFSCorrection) {
		t.Fatalf("coding-task refinement search modes = %+v", coding.RefinementStrategy.SearchModes)
	}
	if coding.RefinementStrategy.BudgetCapTokens <= 0 || !coding.RefinementStrategy.KillSwitchRequired || !coding.RefinementStrategy.HumanApprovalRequired {
		t.Fatalf("coding-task refinement gates incomplete: %+v", coding.RefinementStrategy)
	}
}

func TestProjectTemplateOverridesBuiltInAndRecordsSource(t *testing.T) {
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
  "risk": "medium",
  "phases": [{"id":"plan","name":"Plan","goal":"Plan safely"}],
  "providerRoles": ["default","frontier"],
  "budget": {"frontierTokens": 42, "totalTokens": 100},
  "readinessGates": ["provider_configured"],
  "humanGates": ["git_push"],
  "makerChecker": {"mode":"off"},
  "requiredCapabilities": ["read"],
  "statePolicy": "project_override",
  "maxIterations": 2
}`
	if err := os.WriteFile(filepath.Join(dir, "coding-task.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	templates, err := LoadTemplates(root)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	got, ok := FindTemplate(templates, "coding-task")
	if !ok {
		t.Fatalf("coding-task missing after override: %v", templates)
	}
	if got.Name != "Project Coding" || got.Source != "project" || got.Budget.FrontierTokens != 42 {
		t.Fatalf("override not applied: %+v", got)
	}
	if got.Hash == "" || got.SourcePath == "" {
		t.Fatalf("override should expose hash/source path: %+v", got)
	}
}

func TestRegistryRejectsInvalidProjectTemplates(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "unknown schema version",
			body: `{
  "schemaVersion": "v2",
  "id": "bad",
  "name": "Bad",
  "goal": "Bad",
  "risk": "low",
  "phases": [{"id":"same","name":"A","goal":"A"},{"id":"same","name":"B","goal":"B"}],
  "providerRoles": ["default"],
  "budget": {"frontierTokens": -1},
  "readinessGates": ["provider_configured"],
  "humanGates": [],
  "makerChecker": {"mode":"off"},
  "requiredCapabilities": ["read"],
  "statePolicy": "bad",
  "maxIterations": 1
}`,
		},
		{
			name: "duplicate phase",
			body: `{
  "schemaVersion": "v1",
  "id": "bad",
  "name": "Bad",
  "goal": "Bad",
  "risk": "low",
  "phases": [{"id":"same","name":"A","goal":"A"},{"id":"same","name":"B","goal":"B"}],
  "providerRoles": ["default"],
  "budget": {"frontierTokens": 1},
  "readinessGates": ["provider_configured"],
  "humanGates": [],
  "makerChecker": {"mode":"off"},
  "requiredCapabilities": ["read"],
  "statePolicy": "bad",
  "maxIterations": 1
}`,
		},
		{
			name: "negative budget",
			body: `{
  "schemaVersion": "v1",
  "id": "bad",
  "name": "Bad",
  "goal": "Bad",
  "risk": "low",
  "phases": [{"id":"one","name":"One","goal":"One"}],
  "providerRoles": ["default"],
  "budget": {"frontierTokens": -1},
  "readinessGates": ["provider_configured"],
  "humanGates": [],
  "makerChecker": {"mode":"off"},
  "requiredCapabilities": ["read"],
  "statePolicy": "bad",
  "maxIterations": 1
}`,
		},
		{
			name: "empty goal",
			body: `{
  "schemaVersion": "v1",
  "id": "bad",
  "name": "Bad",
  "goal": " ",
  "risk": "low",
  "phases": [{"id":"one","name":"One","goal":"One"}],
  "providerRoles": ["default"],
  "budget": {"frontierTokens": 1},
  "readinessGates": ["provider_configured"],
  "humanGates": [],
  "makerChecker": {"mode":"off"},
  "requiredCapabilities": ["read"],
  "statePolicy": "bad",
  "maxIterations": 1
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".maddog", "loops")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := LoadTemplates(root); err == nil {
				t.Fatal("LoadTemplates succeeded, want validation error")
			}
		})
	}
}

func TestRegistryAllowsUnknownProviderRoleForReadiness(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".maddog", "loops")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := `{
  "schemaVersion": "v1",
  "id": "custom-role",
  "name": "Custom Role",
  "goal": "Keep unresolved provider roles loadable for readiness",
  "risk": "medium",
  "phases": [{"id":"ready","name":"Ready","goal":"Check role later"}],
  "providerRoles": ["default","frontier-that-is-not-configured"],
  "budget": {"frontierTokens": 1},
  "readinessGates": ["provider_configured"],
  "humanGates": ["git_push"],
  "makerChecker": {"mode":"off"},
  "requiredCapabilities": ["read"],
  "statePolicy": "workspace",
  "maxIterations": 1
}`
	if err := os.WriteFile(filepath.Join(dir, "custom-role.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	templates, err := LoadTemplates(root)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	got, ok := FindTemplate(templates, "custom-role")
	if !ok {
		t.Fatalf("custom-role missing: %v", templates)
	}
	if !contains(got.ProviderRoles, "frontier-that-is-not-configured") {
		t.Fatalf("provider role was rewritten or dropped: %+v", got.ProviderRoles)
	}
}

func contains[T comparable](items []T, want T) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func artifactMapsTo(items []TemplateArtifactMapping, artifact, field string) bool {
	for _, item := range items {
		if item.Artifact == artifact && item.ReportField == field {
			return true
		}
	}
	return false
}
