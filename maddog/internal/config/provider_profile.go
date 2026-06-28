package config

import (
	"net/url"
	"sort"
	"strings"
)

const (
	CredentialConfigured    = "configured"
	CredentialMissing       = "missing"
	CredentialNotConfigured = "not_configured"

	ProviderGatewayOfficialOpenAI      = "official_openai"
	ProviderGatewayOfficialAnthropic   = "official_anthropic"
	ProviderGatewayICodeEasy           = "icodeeasy"
	ProviderGatewayOpenAICompatible    = "openai_compatible"
	ProviderGatewayAnthropicCompatible = "anthropic_compatible"
	ProviderGatewayCustom              = "custom"
)

type ProviderProfileSnapshot struct {
	Profiles []ProviderProfile     `json:"profiles"`
	Warnings []ProviderRoleWarning `json:"warnings,omitempty"`
}

type ProviderProfile struct {
	Name                  string            `json:"name"`
	Roles                 []string          `json:"roles"`
	RoleModels            map[string]string `json:"roleModels"`
	AuthMode              string            `json:"authMode"`
	CredentialEnv         string            `json:"credentialEnv,omitempty"`
	OfficialAuthProfileID string            `json:"officialAuthProfileId,omitempty"`
	CredentialStatus      string            `json:"credentialStatus"`
	Gateway               string            `json:"gateway"`
	FrontierEligible      bool              `json:"frontierEligible"`
	SmallModelEligible    bool              `json:"smallModelEligible"`
	BudgetEligible        bool              `json:"budgetEligible"`
	StatusURL             string            `json:"statusUrl,omitempty"`
	BalanceURL            string            `json:"balanceUrl,omitempty"`
	Warnings              []string          `json:"warnings,omitempty"`
}

type ProviderRoleWarning struct {
	Role    string `json:"role"`
	Ref     string `json:"ref"`
	Message string `json:"message"`
}

func (c *Config) ProviderProfiles() ProviderProfileSnapshot {
	if c == nil {
		return ProviderProfileSnapshot{Profiles: []ProviderProfile{}}
	}
	profiles := make([]ProviderProfile, 0, len(c.Providers))
	byName := map[string]*ProviderProfile{}
	for i := range c.Providers {
		entry := &c.Providers[i]
		profiles = append(profiles, providerProfileBase(*entry))
		byName[entry.Name] = &profiles[len(profiles)-1]
	}
	snapshot := ProviderProfileSnapshot{Profiles: profiles}
	addRole := func(role, ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		entry, ok := c.ResolveModel(ref)
		if !ok {
			snapshot.Warnings = append(snapshot.Warnings, ProviderRoleWarning{
				Role:    role,
				Ref:     ref,
				Message: "model reference does not resolve to a configured provider",
			})
			return
		}
		profile := byName[entry.Name]
		if profile == nil {
			return
		}
		canonical := entry.Name + "/" + entry.Model
		if !containsProfileRole(profile.Roles, role) {
			profile.Roles = append(profile.Roles, role)
		}
		if profile.RoleModels == nil {
			profile.RoleModels = map[string]string{}
		}
		profile.RoleModels[role] = canonical
	}

	addRole("default", c.DefaultModel)
	if maker := c.Agent.SubagentModels["maker"]; strings.TrimSpace(maker) != "" {
		addRole("maker", maker)
	} else {
		addRole("maker", c.DefaultModel)
	}
	addRole("small", c.Agent.SubagentModel)
	addRole("frontier", c.Agent.FrontierModel)
	if advisor := c.Agent.SubagentModels["advisor"]; strings.TrimSpace(advisor) != "" {
		addRole("advisor", advisor)
	}
	if checker := c.Agent.SubagentModels["checker"]; strings.TrimSpace(checker) != "" {
		addRole("checker", checker)
	}

	for i := range snapshot.Profiles {
		finalizeProviderProfile(&snapshot.Profiles[i], c.Agent.FrontierBudget)
	}
	return snapshot
}

func providerProfileBase(entry ProviderEntry) ProviderProfile {
	env := entry.AuthEnvName()
	return ProviderProfile{
		Name:                  entry.Name,
		Roles:                 []string{},
		RoleModels:            map[string]string{},
		AuthMode:              entry.NormalizedAuthType(),
		CredentialEnv:         env,
		OfficialAuthProfileID: strings.TrimSpace(entry.OfficialAuthProfileID),
		CredentialStatus:      providerCredentialStatus(entry, env),
		Gateway:               providerGateway(entry),
		StatusURL:             "",
		BalanceURL:            strings.TrimSpace(entry.BalanceURL),
	}
}

func providerCredentialStatus(entry ProviderEntry, env string) string {
	if strings.TrimSpace(entry.OfficialAuthProfileID) != "" {
		return CredentialConfigured
	}
	if strings.TrimSpace(env) == "" && strings.TrimSpace(entry.IdentityFile) == "" {
		return CredentialNotConfigured
	}
	if entry.Configured() {
		return CredentialConfigured
	}
	return CredentialMissing
}

func providerGateway(entry ProviderEntry) string {
	kind := strings.ToLower(strings.TrimSpace(entry.Kind))
	host := providerProfileHost(entry.BaseURL)
	if strings.Contains(host, "icodeeasy") {
		return ProviderGatewayICodeEasy
	}
	switch kind {
	case "openai":
		if host == "api.openai.com" {
			return ProviderGatewayOfficialOpenAI
		}
		return ProviderGatewayOpenAICompatible
	case "anthropic":
		if host == "api.anthropic.com" {
			return ProviderGatewayOfficialAnthropic
		}
		return ProviderGatewayAnthropicCompatible
	default:
		if kind != "" {
			return kind
		}
		return ProviderGatewayCustom
	}
}

func providerProfileHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func finalizeProviderProfile(profile *ProviderProfile, frontierBudget int64) {
	sort.Strings(profile.Roles)
	if containsProfileRole(profile.Roles, "frontier") || containsProfileRole(profile.Roles, "advisor") || containsProfileRole(profile.Roles, "checker") {
		profile.FrontierEligible = true
		profile.BudgetEligible = frontierBudget > 0
	}
	if containsProfileRole(profile.Roles, "small") {
		profile.SmallModelEligible = true
	}
}

func containsProfileRole(roles []string, want string) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}
