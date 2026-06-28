package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderProfilesDeriveRolesAuthAndGateway(t *testing.T) {
	t.Setenv("OPENAI_MAIN_KEY", "sk-openai-secret")
	t.Setenv("ANTHROPIC_FRONTIER_KEY", "sk-anthropic-secret")

	cfg := Config{
		DefaultModel: "openai-main/gpt-4o",
		Agent: AgentConfig{
			FrontierModel:  "anthropic-frontier/claude-sonnet-4",
			FrontierBudget: 123456,
			SubagentModel:  "icodeeasy-small/qwen2.5-coder",
			SubagentModels: map[string]string{
				"advisor": "anthropic-frontier/claude-sonnet-4",
				"maker":   "openai-main/gpt-4o",
				"checker": "anthropic-frontier/claude-sonnet-4",
			},
		},
		Providers: []ProviderEntry{
			{Name: "openai-main", Kind: "openai", BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-4o"}, APIKeyEnv: "OPENAI_MAIN_KEY"},
			{Name: "anthropic-frontier", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Models: []string{"claude-sonnet-4"}, APIKeyEnv: "ANTHROPIC_FRONTIER_KEY"},
			{Name: "icodeeasy-small", Kind: "openai", BaseURL: "https://gateway.icodeeasy.com/v1", Models: []string{"qwen2.5-coder"}, APIKeyEnv: "ICODEEASY_KEY"},
		},
	}

	snapshot := cfg.ProviderProfiles()
	if len(snapshot.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", snapshot.Warnings)
	}

	openai := findProviderProfile(snapshot.Profiles, "openai-main")
	if openai == nil {
		t.Fatalf("openai-main profile missing: %+v", snapshot.Profiles)
	}
	if !profileHasRole(*openai, "default") || !profileHasRole(*openai, "maker") {
		t.Fatalf("openai-main roles = %v, want default+maker", openai.Roles)
	}
	if openai.RoleModels["default"] != "openai-main/gpt-4o" || openai.Gateway != ProviderGatewayOfficialOpenAI {
		t.Fatalf("openai-main profile = %+v", openai)
	}
	if openai.AuthMode != "api_key" || openai.CredentialStatus != CredentialConfigured || openai.CredentialEnv != "OPENAI_MAIN_KEY" {
		t.Fatalf("openai auth profile = %+v", openai)
	}

	frontier := findProviderProfile(snapshot.Profiles, "anthropic-frontier")
	if frontier == nil {
		t.Fatalf("anthropic-frontier profile missing: %+v", snapshot.Profiles)
	}
	for _, role := range []string{"frontier", "advisor", "checker"} {
		if !profileHasRole(*frontier, role) {
			t.Fatalf("frontier roles = %v, missing %s", frontier.Roles, role)
		}
	}
	if frontier.Gateway != ProviderGatewayOfficialAnthropic || !frontier.FrontierEligible || !frontier.BudgetEligible {
		t.Fatalf("frontier eligibility/profile = %+v", frontier)
	}

	small := findProviderProfile(snapshot.Profiles, "icodeeasy-small")
	if small == nil {
		t.Fatalf("icodeeasy-small profile missing: %+v", snapshot.Profiles)
	}
	if !profileHasRole(*small, "small") || small.Gateway != ProviderGatewayICodeEasy || !small.SmallModelEligible {
		t.Fatalf("small provider profile = %+v", small)
	}
	if small.CredentialStatus != CredentialMissing || small.CredentialEnv != "ICODEEASY_KEY" {
		t.Fatalf("small credential profile = %+v", small)
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-openai-secret") || strings.Contains(string(raw), "sk-anthropic-secret") {
		t.Fatalf("provider profile leaked credential: %s", raw)
	}
}

func TestProviderProfilesWarnOnUnresolvedRoleModel(t *testing.T) {
	cfg := Default()
	cfg.Agent.FrontierModel = "missing-frontier/claude"

	snapshot := cfg.ProviderProfiles()
	if len(snapshot.Warnings) == 0 {
		t.Fatalf("warnings = none, want unresolved frontier warning")
	}
	got := snapshot.Warnings[0]
	if got.Role != "frontier" || got.Ref != "missing-frontier/claude" {
		t.Fatalf("warning = %+v, want frontier missing ref", got)
	}
}

func TestProviderProfilesExposeOfficialAuthProfileWithoutToken(t *testing.T) {
	t.Setenv("OPENAI_ACCESS_TOKEN", "sk-official-token")
	cfg := Config{
		DefaultModel: "openai-official/gpt-5",
		Providers: []ProviderEntry{{
			Name:                  "openai-official",
			Kind:                  "openai",
			BaseURL:               "https://api.openai.com/v1",
			Models:                []string{"gpt-5"},
			AuthType:              "official_auth",
			BearerTokenEnv:        "OPENAI_ACCESS_TOKEN",
			OfficialAuthProfileID: "openai-desktop",
		}},
	}

	snapshot := cfg.ProviderProfiles()
	profile := findProviderProfile(snapshot.Profiles, "openai-official")
	if profile == nil {
		t.Fatalf("openai-official profile missing: %+v", snapshot.Profiles)
	}
	if profile.AuthMode != "official_auth" || profile.CredentialEnv != "OPENAI_ACCESS_TOKEN" || profile.OfficialAuthProfileID != "openai-desktop" {
		t.Fatalf("official auth profile fields = %+v", profile)
	}
	if profile.CredentialStatus != CredentialConfigured {
		t.Fatalf("credential status = %q, want configured", profile.CredentialStatus)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-official-token") {
		t.Fatalf("provider profile leaked official auth token: %s", raw)
	}
}

func findProviderProfile(items []ProviderProfile, name string) *ProviderProfile {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

func profileHasRole(profile ProviderProfile, role string) bool {
	for _, got := range profile.Roles {
		if got == role {
			return true
		}
	}
	return false
}
