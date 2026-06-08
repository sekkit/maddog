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
