package config

import "testing"

func TestDefaultAutoPlanOff(t *testing.T) {
	if got := Default().Agent.AutoPlan; got != "off" {
		t.Fatalf("default auto_plan = %q, want off", got)
	}
}

func TestDefaultRuntimeSkillOrchestrationOn(t *testing.T) {
	cfg := Default()
	if !cfg.Skills.RuntimeOrchestration {
		t.Fatal("runtime skill orchestration should default on")
	}
	if !cfg.Skills.DynamicSkills {
		t.Fatal("dynamic skill generation should default on")
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
