package secrets

import (
	"runtime"
	"strings"
	"testing"
)

func TestProcessEnvRemovesRegisteredCredentialsCaseInsensitively(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("environment names are case-insensitive only on Windows")
	}
	const key = "MADDOG_TEST_STALE_CREDENTIAL"
	t.Setenv(strings.ToLower(key), "stored-secret")
	t.Setenv("MADDOG_TEST_VISIBLE", "ordinary")
	RegisterCredentialEnvKeys([]string{key})

	env := ProcessEnv()
	for _, entry := range env {
		if strings.EqualFold(strings.SplitN(entry, "=", 2)[0], key) {
			t.Fatalf("registered credential leaked to child environment: %q", entry)
		}
	}
	if !containsEnv(env, "MADDOG_TEST_VISIBLE", "ordinary") {
		t.Fatal("ordinary environment variable was removed")
	}
}

func TestProcessEnvKeepsUnixCaseDistinctVariables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows environment names are case-insensitive")
	}
	const key = "MADDOG_CASE_SENSITIVE_SECRET"
	const distinct = "maddog_case_sensitive_secret"
	t.Setenv(distinct, "keep-me")
	RegisterCredentialEnvKeys([]string{key})
	if !containsExactEnv(ProcessEnv(), distinct, "keep-me") {
		t.Fatalf("case-distinct Unix environment variable was filtered")
	}
}

func containsEnv(env []string, key, value string) bool {
	for _, entry := range env {
		name, got, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(name, key) && got == value {
			return true
		}
	}
	return false
}

func containsExactEnv(env []string, key, value string) bool {
	for _, entry := range env {
		name, got, ok := strings.Cut(entry, "=")
		if ok && name == key && got == value {
			return true
		}
	}
	return false
}
