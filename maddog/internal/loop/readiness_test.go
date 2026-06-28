package loop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluateReadinessReady(t *testing.T) {
	tmpl := readinessTemplate()
	result := EvaluateReadiness(ReadinessInput{
		Template:               tmpl,
		ProviderRoles:          readyRoleProfiles(tmpl.ProviderRoles),
		AuthorizedCapabilities: []Capability{CapabilityRead, CapabilityWrite, CapabilityGit, CapabilityProcess},
		BudgetAvailable:        true,
		LogSinkWritable:        true,
		KillSwitchEnabled:      true,
		HumanGatePolicyDefined: true,
		WorkspaceKnown:         true,
		RunID:                  "run-ready",
	})

	if result.Status != ReadinessReady || len(result.Blockers) != 0 {
		t.Fatalf("readiness = %+v, want ready without blockers", result)
	}
	if result.TemplateID != "coding-task" || result.RunID != "run-ready" || result.Score <= 0 {
		t.Fatalf("readiness metadata = %+v", result)
	}
	if !hasCheckStatus(result, "credential_available", CheckPassed) || !hasCheckStatus(result, "capability:git", CheckPassed) {
		t.Fatalf("expected credential and capability checks to pass: %+v", result.Checks)
	}
}

func TestEvaluateReadinessBlocksMissingCriticalInputsWithoutSecretLeak(t *testing.T) {
	tmpl := readinessTemplate()
	result := EvaluateReadiness(ReadinessInput{
		Template: tmpl,
		ProviderRoles: []ProviderRoleProfile{
			{Role: "default", Provider: "openai-main", ModelRef: "openai-main/gpt-4o", CredentialEnv: "OPENAI_API_KEY", CredentialStatus: "missing"},
		},
		AuthorizedCapabilities: []Capability{CapabilityRead},
		BudgetAvailable:        false,
		LogSinkWritable:        false,
		KillSwitchEnabled:      false,
		HumanGatePolicyDefined: false,
		WorkspaceKnown:         true,
	})

	if result.Status != ReadinessBlocked {
		t.Fatalf("status = %q, want blocked: %+v", result.Status, result)
	}
	for _, id := range []string{"credential_available", "budget_available", "log_sink_writable", "kill_switch_enabled", "human_gate_policy", "capability:write"} {
		if !hasCheckStatus(result, id, CheckBlocked) {
			t.Fatalf("missing blocked check %s in %+v", id, result.Checks)
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret") {
		t.Fatalf("readiness leaked token value: %s", raw)
	}
	if !strings.Contains(string(raw), "OPENAI_API_KEY") {
		t.Fatalf("readiness should expose credential env reference, got %s", raw)
	}
}

func TestEvaluateReadinessWarnsForFallbackCodeBackend(t *testing.T) {
	tmpl := readinessTemplate()
	tmpl.ReadinessGates = append(tmpl.ReadinessGates, "required_code_backend_available")

	result := EvaluateReadiness(ReadinessInput{
		Template:                   tmpl,
		ProviderRoles:              readyRoleProfiles(tmpl.ProviderRoles),
		AuthorizedCapabilities:     []Capability{CapabilityRead, CapabilityWrite, CapabilityGit, CapabilityProcess},
		BudgetAvailable:            true,
		LogSinkWritable:            true,
		KillSwitchEnabled:          true,
		HumanGatePolicyDefined:     true,
		WorkspaceKnown:             true,
		CodeBackendAvailable:       false,
		CodeBackendFallbackAllowed: true,
	})

	if result.Status != ReadinessWarning {
		t.Fatalf("status = %q, want warning: %+v", result.Status, result)
	}
	if len(result.Warnings) == 0 || !hasCheckStatus(result, "required_code_backend_available", CheckWarning) {
		t.Fatalf("expected code backend warning: %+v", result)
	}
}

func TestEvaluateReadinessNeedsApprovalForPendingCapability(t *testing.T) {
	tmpl := readinessTemplate()
	result := EvaluateReadiness(ReadinessInput{
		Template:               tmpl,
		ProviderRoles:          readyRoleProfiles(tmpl.ProviderRoles),
		AuthorizedCapabilities: []Capability{CapabilityRead, CapabilityWrite, CapabilityProcess},
		PendingCapabilities:    []Capability{CapabilityGit},
		BudgetAvailable:        true,
		LogSinkWritable:        true,
		KillSwitchEnabled:      true,
		HumanGatePolicyDefined: true,
		WorkspaceKnown:         true,
	})

	if result.Status != ReadinessNeedsApproval {
		t.Fatalf("status = %q, want needs_approval: %+v", result.Status, result)
	}
	if !hasCheckStatus(result, "capability:git", CheckNeedsApproval) {
		t.Fatalf("git capability should require approval: %+v", result.Checks)
	}
}

func readinessTemplate() LoopTemplateV1 {
	templates, err := BuiltInTemplates()
	if err != nil {
		panic(err)
	}
	tmpl, ok := FindTemplate(templates, "coding-task")
	if !ok {
		panic("coding-task template missing")
	}
	return tmpl
}

func readyRoleProfiles(roles []string) []ProviderRoleProfile {
	out := make([]ProviderRoleProfile, 0, len(roles))
	for _, role := range roles {
		out = append(out, ProviderRoleProfile{
			Role:               role,
			Provider:           role + "-provider",
			ModelRef:           role + "-provider/model",
			CredentialEnv:      strings.ToUpper(role) + "_API_KEY",
			CredentialStatus:   CredentialConfigured,
			BudgetEligible:     true,
			FrontierEligible:   role == "frontier" || role == "advisor" || role == "checker",
			SmallModelEligible: role == "small",
		})
	}
	return out
}

func hasCheckStatus(result ReadinessResult, id string, status ReadinessCheckStatus) bool {
	for _, check := range result.Checks {
		if check.ID == id && check.Status == status {
			return true
		}
	}
	return false
}
