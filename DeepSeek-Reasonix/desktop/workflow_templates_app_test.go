package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/loop"
)

func TestWorkflowTemplatesReturnsBuiltIns(t *testing.T) {
	got, err := NewApp().WorkflowTemplatesForRoot("")
	if err != nil {
		t.Fatalf("WorkflowTemplatesForRoot: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("template count = %d, want 3", len(got))
	}
	coding := findWorkflowTemplate(got, "coding-task")
	if coding == nil {
		t.Fatalf("coding-task missing: %+v", got)
	}
	if coding.SchemaVersion != "v1" || coding.Name == "" || coding.Risk == "" {
		t.Fatalf("coding-task incomplete: %+v", coding)
	}
	if coding.Source != "built-in" || coding.Hash == "" {
		t.Fatalf("coding-task source/hash = %q/%q, want built-in/hash", coding.Source, coding.Hash)
	}
	if len(coding.ProviderRoles) == 0 || len(coding.RequiredCapabilities) == 0 || len(coding.HumanGates) == 0 {
		t.Fatalf("coding-task missing role/capability/gate metadata: %+v", coding)
	}
	if len(coding.Artifacts.TaskPacketFields) == 0 || coding.Artifacts.BoundedFanOut.MaxParallel == 0 {
		t.Fatalf("coding-task missing workflow artifact contract: %+v", coding.Artifacts)
	}
	if !workflowArtifactMapsTo(coding.Artifacts.RunReportMapping, "final_verification", "report.finalStatus") {
		t.Fatalf("coding-task missing run report artifact mapping: %+v", coding.Artifacts.RunReportMapping)
	}
	if coding.RefinementStrategy.Enabled || coding.RefinementStrategy.BudgetCapTokens == 0 || !workflowContainsString(coding.RefinementStrategy.SearchModes, "bfs_hypothesis") {
		t.Fatalf("coding-task refinement strategy should be default-off and gated: %+v", coding.RefinementStrategy)
	}
}

func TestWorkflowTemplatesForRootShowsProjectOverride(t *testing.T) {
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

	got, err := NewApp().WorkflowTemplatesForRoot(root)
	if err != nil {
		t.Fatalf("WorkflowTemplatesForRoot: %v", err)
	}
	coding := findWorkflowTemplate(got, "coding-task")
	if coding == nil {
		t.Fatalf("coding-task missing: %+v", got)
	}
	if coding.Name != "Project Coding" || coding.Source != "project" || coding.SourcePath == "" || coding.Hash == "" {
		t.Fatalf("project override not exposed: %+v", coding)
	}
}

func TestWorkflowReadinessForRootReturnsReady(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("OPENAI_MAIN_KEY", "sk-openai-secret")
	t.Setenv("SMALL_KEY", "sk-small-secret")
	t.Setenv("ANTHROPIC_KEY", "sk-anthropic-secret")
	writeWorkflowReadinessConfig(t)

	got, err := NewApp().WorkflowReadinessForRoot("", "coding-task")
	if err != nil {
		t.Fatalf("WorkflowReadinessForRoot: %v", err)
	}
	if got.Status != loop.ReadinessReady {
		t.Fatalf("readiness = %+v, want ready", got)
	}
	if !workflowHasCheckStatus(got, "credential_available", loop.CheckPassed) {
		t.Fatalf("credential check missing/pass expected: %+v", got.Checks)
	}
}

func TestWorkflowReadinessForRootBlocksMissingCredentialWithoutSecrets(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("OPENAI_MAIN_KEY", "sk-openai-secret")
	t.Setenv("ANTHROPIC_KEY", "sk-anthropic-secret")
	writeWorkflowReadinessConfig(t)

	got, err := NewApp().WorkflowReadinessForRoot("", "coding-task")
	if err != nil {
		t.Fatalf("WorkflowReadinessForRoot: %v", err)
	}
	if got.Status != loop.ReadinessBlocked {
		t.Fatalf("readiness = %+v, want blocked", got)
	}
	if !workflowHasCheckStatus(got, "credential_available", loop.CheckBlocked) {
		t.Fatalf("credential blocker missing: %+v", got.Checks)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-openai-secret") || strings.Contains(string(raw), "sk-anthropic-secret") {
		t.Fatalf("readiness leaked token value: %s", raw)
	}
	if !strings.Contains(string(raw), "SMALL_KEY") {
		t.Fatalf("readiness should expose missing credential env reference: %s", raw)
	}
}

func findWorkflowTemplate(items []WorkflowTemplateView, id string) *WorkflowTemplateView {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func writeWorkflowReadinessConfig(t *testing.T) {
	t.Helper()
	cfg := config.Default()
	cfg.DefaultModel = "openai-main/gpt-4o"
	cfg.Agent.FrontierModel = "anthropic-frontier/claude-sonnet-4"
	cfg.Agent.FrontierBudget = 1000
	cfg.Agent.SubagentModel = "small/qwen2.5-coder"
	cfg.Agent.SubagentModels = map[string]string{
		"advisor": "anthropic-frontier/claude-sonnet-4",
		"maker":   "openai-main/gpt-4o",
		"checker": "anthropic-frontier/claude-sonnet-4",
	}
	cfg.Providers = []config.ProviderEntry{
		{Name: "openai-main", Kind: "openai", BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-4o"}, APIKeyEnv: "OPENAI_MAIN_KEY"},
		{Name: "small", Kind: "openai", BaseURL: "https://gateway.icodeeasy.com/v1", Models: []string{"qwen2.5-coder"}, APIKeyEnv: "SMALL_KEY"},
		{Name: "anthropic-frontier", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Models: []string{"claude-sonnet-4"}, APIKeyEnv: "ANTHROPIC_KEY"},
	}
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
}

func workflowHasCheckStatus(result loop.ReadinessResult, id string, status loop.ReadinessCheckStatus) bool {
	for _, check := range result.Checks {
		if check.ID == id && check.Status == status {
			return true
		}
	}
	return false
}

func workflowArtifactMapsTo(items []WorkflowArtifactMappingView, artifact, field string) bool {
	for _, item := range items {
		if item.Artifact == artifact && item.ReportField == field {
			return true
		}
	}
	return false
}

func workflowContainsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
