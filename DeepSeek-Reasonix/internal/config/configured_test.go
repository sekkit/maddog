package config

import "testing"

// TestProviderConfigured verifies Configured tracks whether the api_key_env
// resolves to a non-empty value — the same key check Validate enforces at build
// time, so model pickers can filter on it.
func TestProviderConfigured(t *testing.T) {
	t.Setenv("REASONIX_TEST_KEY", "secret")
	t.Setenv("REASONIX_TEST_TOKEN", "token")
	t.Setenv("REASONIX_TEST_EMPTY", "")

	cases := []struct {
		name string
		p    ProviderEntry
		want bool
	}{
		{"key set", ProviderEntry{APIKeyEnv: "REASONIX_TEST_KEY"}, true},
		{"key env empty", ProviderEntry{APIKeyEnv: "REASONIX_TEST_EMPTY"}, false},
		{"key env unset", ProviderEntry{APIKeyEnv: "REASONIX_TEST_MISSING"}, false},
		{"bearer token set", ProviderEntry{AuthType: "bearer", AuthTokenEnv: "REASONIX_TEST_TOKEN"}, true},
		{"bearer token unset", ProviderEntry{AuthType: "bearer", AuthTokenEnv: "REASONIX_TEST_MISSING"}, false},
		{"no api_key_env", ProviderEntry{}, false},
	}
	for _, c := range cases {
		if got := c.p.Configured(); got != c.want {
			t.Errorf("%s: Configured() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProviderAuthMaterial(t *testing.T) {
	t.Setenv("REASONIX_TEST_KEY", "secret")
	t.Setenv("REASONIX_TEST_TOKEN", "token")

	apiKey := ProviderEntry{APIKeyEnv: "REASONIX_TEST_KEY"}
	if got := apiKey.AuthToken(); got != "secret" {
		t.Fatalf("api-key AuthToken = %q, want secret", got)
	}
	if got := apiKey.NormalizedAuthType(); got != "api_key" {
		t.Fatalf("api-key NormalizedAuthType = %q", got)
	}

	bearer := ProviderEntry{AuthType: "workload_identity", AuthTokenEnv: "REASONIX_TEST_TOKEN"}
	if got := bearer.AuthToken(); got != "token" {
		t.Fatalf("bearer AuthToken = %q, want token", got)
	}
	if got := bearer.NormalizedAuthType(); got != "workload_identity" {
		t.Fatalf("bearer NormalizedAuthType = %q", got)
	}
	if got := bearer.AuthEnvName(); got != "REASONIX_TEST_TOKEN" {
		t.Fatalf("bearer AuthEnvName = %q", got)
	}

	wifAssertion := ProviderEntry{AuthType: "workload_identity", IdentityEnv: "REASONIX_TEST_TOKEN"}
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
		IdentityEnv:        "REASONIX_TEST_TOKEN",
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
