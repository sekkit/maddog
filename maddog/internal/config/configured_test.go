package config

import (
	"strings"
	"testing"
)

func TestDefaultIncludesResolvablePxpipeClaudeProvider(t *testing.T) {
	cfg := Default()
	claude, ok := cfg.Provider("pxpipe-claude")
	if !ok {
		t.Fatal("default config missing pxpipe-claude provider")
	}
	if claude.Kind != "anthropic" || claude.BaseURL != "http://127.0.0.1:47821" {
		t.Fatalf("pxpipe-claude kind/base_url = %q/%q, want anthropic local pxpipe", claude.Kind, claude.BaseURL)
	}
	if claude.Model != "claude-fable-5" || claude.APIKeyEnv != "ICE_API_KEY" {
		t.Fatalf("pxpipe-claude model/key = %q/%q, want claude-fable-5/ICE_API_KEY", claude.Model, claude.APIKeyEnv)
	}
	if claude.ClientProfile != "claude_code" || claude.ClientVersion == "" {
		t.Fatalf("pxpipe-claude client profile/version = %q/%q, want claude_code with version", claude.ClientProfile, claude.ClientVersion)
	}
	resolved, ok := cfg.ResolveModel("pxpipe-claude")
	if !ok {
		t.Fatal("ResolveModel(pxpipe-claude) failed")
	}
	if resolved.Name != "pxpipe-claude" || resolved.Model != "claude-fable-5" {
		t.Fatalf("resolved pxpipe-claude = %s/%s, want pxpipe-claude/claude-fable-5", resolved.Name, resolved.Model)
	}
}

func TestDefaultPxpipeGPTRequiresDiscoveredOrConfiguredModel(t *testing.T) {
	cfg := Default()
	gpt, ok := cfg.Provider("pxpipe-gpt")
	if !ok {
		t.Fatal("default config missing pxpipe-gpt provider")
	}
	if gpt.Kind != "openai" || gpt.BaseURL != "http://127.0.0.1:47821/v1" {
		t.Fatalf("pxpipe-gpt kind/base_url = %q/%q, want openai local pxpipe", gpt.Kind, gpt.BaseURL)
	}
	if len(gpt.ModelList()) != 0 {
		t.Fatalf("pxpipe-gpt models = %v, want empty list populated only by provider /models response", gpt.ModelList())
	}
	if gpt.WireAPI != "responses" || gpt.ReasoningProtocol != "openai" {
		t.Fatalf("pxpipe-gpt wire/reasoning = %q/%q, want responses/openai", gpt.WireAPI, gpt.ReasoningProtocol)
	}
	if _, ok := cfg.ResolveModel("pxpipe-gpt"); ok {
		t.Fatal("ResolveModel(pxpipe-gpt) succeeded with no configured or discovered model")
	}
	t.Setenv("ICE_API_KEY", "test-key")
	err := cfg.Validate("pxpipe-gpt")
	if err == nil || !strings.Contains(err.Error(), "no models configured") {
		t.Fatalf("Validate(pxpipe-gpt) = %v, want actionable no-models error", err)
	}
	gpt.Models = []string{"gpt-5.6"}
	gpt.Default = "gpt-5.6"
	resolved, ok := cfg.ResolveModel("pxpipe-gpt/gpt-5.6")
	if !ok {
		t.Fatal("ResolveModel(pxpipe-gpt/gpt-5.6) failed")
	}
	if resolved.Model != "gpt-5.6" || resolved.WireAPI != "responses" {
		t.Fatalf("resolved pxpipe-gpt = model %q wire %q, want gpt-5.6 responses", resolved.Model, resolved.WireAPI)
	}
}

// TestProviderConfigured verifies Configured tracks whether the provider can be
// selected. Providers with no api_key_env are explicit no-auth providers; if an
// env var is configured, it must resolve to a non-empty value.
func TestProviderConfigured(t *testing.T) {
	t.Setenv("MADDOG_TEST_KEY", "secret")
	t.Setenv("MADDOG_TEST_TOKEN", "token")
	t.Setenv("MADDOG_TEST_EMPTY", "")

	cases := []struct {
		name string
		p    ProviderEntry
		want bool
	}{
		{"key set", ProviderEntry{APIKeyEnv: "MADDOG_TEST_KEY"}, true},
		{"key env empty", ProviderEntry{APIKeyEnv: "MADDOG_TEST_EMPTY"}, false},
		{"key env unset", ProviderEntry{APIKeyEnv: "MADDOG_TEST_MISSING"}, false},
		{"bearer token set", ProviderEntry{AuthType: "bearer", AuthTokenEnv: "MADDOG_TEST_TOKEN"}, true},
		{"bearer token unset", ProviderEntry{AuthType: "bearer", AuthTokenEnv: "MADDOG_TEST_MISSING"}, false},
		{"no api_key_env", ProviderEntry{}, false},
	}
	for _, c := range cases {
		if got := c.p.Configured(); got != c.want {
			t.Errorf("%s: Configured() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProviderAuthMaterial(t *testing.T) {
	t.Setenv("MADDOG_TEST_KEY", "secret")
	t.Setenv("MADDOG_TEST_TOKEN", "token")

	apiKey := ProviderEntry{APIKeyEnv: "MADDOG_TEST_KEY"}
	if got := apiKey.AuthToken(); got != "secret" {
		t.Fatalf("api-key AuthToken = %q, want secret", got)
	}
	if got := apiKey.NormalizedAuthType(); got != "api_key" {
		t.Fatalf("api-key NormalizedAuthType = %q", got)
	}

	bearer := ProviderEntry{AuthType: "workload_identity", AuthTokenEnv: "MADDOG_TEST_TOKEN"}
	if got := bearer.AuthToken(); got != "token" {
		t.Fatalf("bearer AuthToken = %q, want token", got)
	}
	if got := bearer.NormalizedAuthType(); got != "workload_identity" {
		t.Fatalf("bearer NormalizedAuthType = %q", got)
	}
	if got := bearer.AuthEnvName(); got != "MADDOG_TEST_TOKEN" {
		t.Fatalf("bearer AuthEnvName = %q", got)
	}

	wifAssertion := ProviderEntry{AuthType: "workload_identity", IdentityEnv: "MADDOG_TEST_TOKEN"}
	if got := wifAssertion.AuthToken(); got != "" {
		t.Fatalf("workload identity assertion-only AuthToken = %q, want empty so provider performs token exchange", got)
	}
	if got := wifAssertion.IdentityToken(); got != "token" {
		t.Fatalf("workload identity assertion = %q, want token", got)
	}
	if !wifAssertion.Configured() {
		t.Fatal("workload identity assertion-only provider should be configured")
	}

	openAIWIF := ProviderEntry{
		AuthType:           "workload_identity",
		IdentityEnv:        "MADDOG_TEST_TOKEN",
		IdentityProviderID: "wip_openai",
		ServiceAcctID:      "svc_openai",
		SubjectTokenType:   "urn:ietf:params:oauth:token-type:id_token",
		TokenURL:           "http://127.0.0.1/oauth/token",
	}
	auth := openAIWIF.AuthConfig()
	for key, want := range map[string]string{
		"identity_provider_id": "wip_openai",
		"service_account_id":   "svc_openai",
		"subject_token_type":   "urn:ietf:params:oauth:token-type:id_token",
		"token_url":            "http://127.0.0.1/oauth/token",
	} {
		if got := auth.Extra[key]; got != want {
			t.Fatalf("AuthConfig.Extra[%q] = %q, want %q", key, got, want)
		}
	}
}
