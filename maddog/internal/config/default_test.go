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

func TestDefaultDynamicSkillsOptIn(t *testing.T) {
	cfg := Default()
	if !cfg.Skills.RuntimeOrchestration {
		t.Fatal("runtime skill orchestration should be enabled by default")
	}
	if cfg.Skills.DynamicSkills {
		t.Fatal("dynamic skill generation must be opt-in to avoid extra model calls on unmatched turns")
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
	if cfg.Agent.AdvisorNativeMaxTokens != 2048 {
		t.Fatalf("advisor_native_max_tokens = %d, want 2048", cfg.Agent.AdvisorNativeMaxTokens)
	}
	if cfg.Agent.AdvisorNativeCacheTTL != "" {
		t.Fatalf("advisor_native_cache_ttl = %q, want disabled", cfg.Agent.AdvisorNativeCacheTTL)
	}
}

func TestNormalizeAdvisorNativeCacheTTL(t *testing.T) {
	tests := []struct {
		in    string
		want  string
		valid bool
	}{
		{in: "", want: "", valid: true},
		{in: " 5M ", want: "5m", valid: true},
		{in: "1h", want: "1h", valid: true},
		{in: "30m", want: "", valid: false},
	}
	for _, tt := range tests {
		got, valid := NormalizeAdvisorNativeCacheTTL(tt.in)
		if got != tt.want || valid != tt.valid {
			t.Errorf("NormalizeAdvisorNativeCacheTTL(%q) = %q, %v; want %q, %v", tt.in, got, valid, tt.want, tt.valid)
		}
	}
}

func TestDefaultContextCompressionPolicy(t *testing.T) {
	cfg := Default()
	if got := cfg.Agent.ContextCompression.Policy; got != "auto" {
		t.Fatalf("context compression policy = %q, want auto", got)
	}
	if cfg.Agent.ContextCompression.ThresholdBytes <= 0 {
		t.Fatalf("context compression threshold = %d, want positive default", cfg.Agent.ContextCompression.ThresholdBytes)
	}
	if cfg.Agent.ContextCompression.MaxBytes <= 0 {
		t.Fatalf("context compression max bytes = %d, want positive default", cfg.Agent.ContextCompression.MaxBytes)
	}
}
