package config

import "testing"

func TestDefaultAutoPlanOff(t *testing.T) {
	if got := Default().Agent.AutoPlan; got != "off" {
		t.Fatalf("default auto_plan = %q, want off", got)
	}
}

func TestDefaultReasoningLanguageAuto(t *testing.T) {
	if got := Default().ReasoningLanguage(); got != "auto" {
		t.Fatalf("default reasoning_language = %q, want auto", got)
	}
}

func TestDefaultAdvisorGuardrails(t *testing.T) {
	cfg := Default()
	if cfg.Agent.AdvisorMaxUsesPerTurn != 1 {
		t.Fatalf("advisor_max_uses_per_turn = %d, want 1", cfg.Agent.AdvisorMaxUsesPerTurn)
	}
	if cfg.Agent.AdvisorMaxUsesPerSession != 10 {
		t.Fatalf("advisor_max_uses_per_session = %d, want 10", cfg.Agent.AdvisorMaxUsesPerSession)
	}
	if cfg.Agent.AdvisorMaxContextMessages != 12 {
		t.Fatalf("advisor_max_context_messages = %d, want 12", cfg.Agent.AdvisorMaxContextMessages)
	}
	if cfg.Agent.AdvisorMaxContextChars != 12000 {
		t.Fatalf("advisor_max_context_chars = %d, want 12000", cfg.Agent.AdvisorMaxContextChars)
	}
	if cfg.Agent.AdvisorNativeEnabled {
		t.Fatal("advisor_native_enabled should default off because it is a provider beta")
	}
}

func TestDefaultLoopConfig(t *testing.T) {
	cfg := Default()
	if !cfg.Loop.Enabled {
		t.Fatal("Loop.Enabled = false, want true")
	}
	if cfg.Loop.DefaultTemplate != "coding-task" {
		t.Fatalf("Loop.DefaultTemplate = %q, want coding-task", cfg.Loop.DefaultTemplate)
	}
	if cfg.Loop.ProjectTemplateDir != ".maddog/loops" {
		t.Fatalf("Loop.ProjectTemplateDir = %q, want .maddog/loops", cfg.Loop.ProjectTemplateDir)
	}
}

func TestDefaultContextPolicyAuto(t *testing.T) {
	cfg := Default()
	if got := cfg.ContextPolicy(); got != "auto" {
		t.Fatalf("default context_policy = %q, want auto", got)
	}
}
