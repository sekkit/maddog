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

	official := ProviderEntry{
		AuthType:              "official_auth",
		BearerTokenEnv:        "REASONIX_TEST_TOKEN",
		OfficialAuthProfileID: "openai-desktop",
	}
	if got := official.AuthToken(); got != "token" {
		t.Fatalf("official AuthToken = %q, want token", got)
	}
	if got := official.NormalizedAuthType(); got != "official_auth" {
		t.Fatalf("official NormalizedAuthType = %q", got)
	}
	if got := official.AuthEnvName(); got != "REASONIX_TEST_TOKEN" {
		t.Fatalf("official AuthEnvName = %q", got)
	}
	if !official.Configured() {
		t.Fatal("official auth profile with bearer token env should be configured")
	}
}
