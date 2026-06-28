package loop

import (
	"fmt"
	"math"
	"strings"
)

const (
	CredentialConfigured    = "configured"
	CredentialMissing       = "missing"
	CredentialNotConfigured = "not_configured"
)

type ReadinessStatus string

const (
	ReadinessReady         ReadinessStatus = "ready"
	ReadinessWarning       ReadinessStatus = "warning"
	ReadinessBlocked       ReadinessStatus = "blocked"
	ReadinessNeedsApproval ReadinessStatus = "needs_approval"
)

type ReadinessCheckStatus string

const (
	CheckPassed        ReadinessCheckStatus = "passed"
	CheckWarning       ReadinessCheckStatus = "warning"
	CheckBlocked       ReadinessCheckStatus = "blocked"
	CheckNeedsApproval ReadinessCheckStatus = "needs_approval"
)

type ProviderRoleProfile struct {
	Role               string `json:"role"`
	Provider           string `json:"provider"`
	ModelRef           string `json:"modelRef"`
	CredentialEnv      string `json:"credentialEnv,omitempty"`
	CredentialStatus   string `json:"credentialStatus"`
	FrontierEligible   bool   `json:"frontierEligible,omitempty"`
	SmallModelEligible bool   `json:"smallModelEligible,omitempty"`
	BudgetEligible     bool   `json:"budgetEligible,omitempty"`
	Gateway            string `json:"gateway,omitempty"`
}

type ReadinessInput struct {
	Template                   LoopTemplateV1
	ProviderRoles              []ProviderRoleProfile
	AuthorizedCapabilities     []Capability
	PendingCapabilities        []Capability
	BudgetAvailable            bool
	LogSinkWritable            bool
	KillSwitchEnabled          bool
	HumanGatePolicyDefined     bool
	WorkspaceKnown             bool
	CodeBackendAvailable       bool
	CodeBackendFallbackAllowed bool
	RunID                      string
}

type ReadinessCheck struct {
	ID            string               `json:"id"`
	Label         string               `json:"label,omitempty"`
	Status        ReadinessCheckStatus `json:"status"`
	Message       string               `json:"message,omitempty"`
	RepairHint    string               `json:"repairHint,omitempty"`
	Role          string               `json:"role,omitempty"`
	Provider      string               `json:"provider,omitempty"`
	ModelRef      string               `json:"modelRef,omitempty"`
	CredentialEnv string               `json:"credentialEnv,omitempty"`
	Capability    Capability           `json:"capability,omitempty"`
}

type ReadinessResult struct {
	Status      ReadinessStatus  `json:"status"`
	Score       int              `json:"score"`
	Checks      []ReadinessCheck `json:"checks"`
	Blockers    []string         `json:"blockers,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	RepairHints []string         `json:"repairHints,omitempty"`
	TemplateID  string           `json:"templateId,omitempty"`
	RunID       string           `json:"runId,omitempty"`
}

func EvaluateReadiness(in ReadinessInput) ReadinessResult {
	result := ReadinessResult{
		Status:     ReadinessReady,
		TemplateID: strings.TrimSpace(in.Template.ID),
		RunID:      strings.TrimSpace(in.RunID),
	}
	roles := map[string]ProviderRoleProfile{}
	for _, profile := range in.ProviderRoles {
		role := strings.TrimSpace(profile.Role)
		if role == "" {
			continue
		}
		profile.Role = role
		roles[role] = profile
	}

	if strings.TrimSpace(in.Template.ID) == "" {
		result.addCheck(ReadinessCheck{
			ID:         "template_configured",
			Status:     CheckBlocked,
			Message:    "workflow template is not configured",
			RepairHint: "select a Maddog workflow template before starting the run",
		})
	}
	for _, gate := range in.Template.ReadinessGates {
		switch strings.TrimSpace(gate) {
		case "provider_configured":
			result.addCheck(providerConfiguredCheck(in.Template.ProviderRoles, roles))
		case "credential_available":
			result.addCheck(credentialAvailableCheck(in.Template.ProviderRoles, roles))
		case "budget_available":
			result.addCheck(booleanGateCheck("budget_available", in.BudgetAvailable, "frontier budget is available", "configure a per-run frontier budget before starting"))
		case "log_sink_writable":
			result.addCheck(booleanGateCheck("log_sink_writable", in.LogSinkWritable, "run log sink is writable", "choose a writable Maddog data directory"))
		case "kill_switch_enabled":
			result.addCheck(booleanGateCheck("kill_switch_enabled", in.KillSwitchEnabled, "kill switch is enabled", "enable turn or loop cancellation before starting"))
		case "human_gate_policy":
			result.addCheck(booleanGateCheck("human_gate_policy", in.HumanGatePolicyDefined, "human gate policy is defined", "configure human gates for risky operations"))
		case "required_code_backend_available":
			result.addCheck(codeBackendCheck(in.CodeBackendAvailable, in.CodeBackendFallbackAllowed))
		}
	}
	if !in.WorkspaceKnown {
		result.addCheck(ReadinessCheck{
			ID:         "workspace_state",
			Status:     CheckWarning,
			Message:    "workspace state is unknown",
			RepairHint: "open or refresh the workspace before starting",
		})
	}
	for _, cap := range in.Template.RequiredCapabilities {
		result.addCheck(capabilityCheck(cap, in.AuthorizedCapabilities, in.PendingCapabilities))
	}
	result.finalize()
	return result
}

func providerConfiguredCheck(required []string, roles map[string]ProviderRoleProfile) ReadinessCheck {
	for _, role := range required {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		profile, ok := roles[role]
		if !ok || strings.TrimSpace(profile.Provider) == "" || strings.TrimSpace(profile.ModelRef) == "" {
			return ReadinessCheck{
				ID:         "provider_configured",
				Status:     CheckBlocked,
				Message:    fmt.Sprintf("provider role %q is not mapped to a configured model", role),
				RepairHint: "configure provider role mappings in Settings",
				Role:       role,
			}
		}
	}
	return ReadinessCheck{ID: "provider_configured", Status: CheckPassed, Message: "all provider roles resolve"}
}

func credentialAvailableCheck(required []string, roles map[string]ProviderRoleProfile) ReadinessCheck {
	for _, role := range required {
		role = strings.TrimSpace(role)
		profile, ok := roles[role]
		if !ok {
			return ReadinessCheck{
				ID:         "credential_available",
				Status:     CheckBlocked,
				Message:    fmt.Sprintf("provider role %q has no credential profile", role),
				RepairHint: "configure provider credentials in Settings",
				Role:       role,
			}
		}
		if strings.TrimSpace(profile.CredentialStatus) != CredentialConfigured {
			return ReadinessCheck{
				ID:            "credential_available",
				Status:        CheckBlocked,
				Message:       fmt.Sprintf("provider role %q credential is %s", role, profile.CredentialStatus),
				RepairHint:    "set the referenced credential before starting",
				Role:          role,
				Provider:      profile.Provider,
				ModelRef:      profile.ModelRef,
				CredentialEnv: profile.CredentialEnv,
			}
		}
	}
	return ReadinessCheck{ID: "credential_available", Status: CheckPassed, Message: "all provider credentials are available"}
}

func booleanGateCheck(id string, ok bool, passed, hint string) ReadinessCheck {
	if ok {
		return ReadinessCheck{ID: id, Status: CheckPassed, Message: passed}
	}
	return ReadinessCheck{ID: id, Status: CheckBlocked, Message: strings.ReplaceAll(id, "_", " ") + " is not ready", RepairHint: hint}
}

func codeBackendCheck(available, fallbackAllowed bool) ReadinessCheck {
	if available {
		return ReadinessCheck{ID: "required_code_backend_available", Status: CheckPassed, Message: "required code intelligence backend is available"}
	}
	if fallbackAllowed {
		return ReadinessCheck{
			ID:         "required_code_backend_available",
			Status:     CheckWarning,
			Message:    "code intelligence backend is unavailable; built-in fallback will be used",
			RepairHint: "repair or disable the external code backend",
		}
	}
	return ReadinessCheck{
		ID:         "required_code_backend_available",
		Status:     CheckBlocked,
		Message:    "required code intelligence backend is unavailable",
		RepairHint: "repair the code intelligence backend before starting",
	}
}

func capabilityCheck(cap Capability, authorized, pending []Capability) ReadinessCheck {
	id := "capability:" + string(cap)
	if containsCapability(authorized, cap) {
		return ReadinessCheck{ID: id, Status: CheckPassed, Message: "capability authorized", Capability: cap}
	}
	if containsCapability(pending, cap) {
		return ReadinessCheck{
			ID:         id,
			Status:     CheckNeedsApproval,
			Message:    "capability requires human approval",
			RepairHint: "approve the capability before starting",
			Capability: cap,
		}
	}
	return ReadinessCheck{
		ID:         id,
		Status:     CheckBlocked,
		Message:    "required capability is not authorized",
		RepairHint: "authorize the required capability or select another workflow",
		Capability: cap,
	}
}

func containsCapability(items []Capability, want Capability) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func (r *ReadinessResult) addCheck(check ReadinessCheck) {
	if check.ID == "" {
		return
	}
	r.Checks = append(r.Checks, check)
	switch check.Status {
	case CheckBlocked:
		r.Blockers = append(r.Blockers, check.Message)
		if check.RepairHint != "" {
			r.RepairHints = append(r.RepairHints, check.RepairHint)
		}
	case CheckNeedsApproval:
		if r.Status != ReadinessBlocked {
			r.Status = ReadinessNeedsApproval
		}
		if check.RepairHint != "" {
			r.RepairHints = append(r.RepairHints, check.RepairHint)
		}
	case CheckWarning:
		r.Warnings = append(r.Warnings, check.Message)
		if r.Status == ReadinessReady {
			r.Status = ReadinessWarning
		}
		if check.RepairHint != "" {
			r.RepairHints = append(r.RepairHints, check.RepairHint)
		}
	}
	if check.Status == CheckBlocked {
		r.Status = ReadinessBlocked
	}
}

func (r *ReadinessResult) finalize() {
	if len(r.Checks) == 0 {
		r.Score = 0
		return
	}
	weight := 0
	for _, check := range r.Checks {
		switch check.Status {
		case CheckPassed:
			weight += 100
		case CheckWarning:
			weight += 70
		case CheckNeedsApproval:
			weight += 50
		}
	}
	r.Score = int(math.Round(float64(weight) / float64(len(r.Checks))))
	if r.Status == "" {
		r.Status = ReadinessReady
	}
}
