package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDotEnvIgnoresHomeEnv proves Maddog no longer imports ~/.env: a project
// .env is still honored, but user-home env files remain owned by the user and
// other installed clients.
func TestLoadDotEnvIgnoresHomeEnv(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()

	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("KEY_CWD=from_cwd\nKEY_SHARED=cwd_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("KEY_HOME=from_home\nKEY_SHARED=home_loses\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(cwd)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.

	// Start clean so the file values are what land (Setenv auto-restores).
	t.Setenv("KEY_CWD", "")
	os.Unsetenv("KEY_CWD")
	t.Setenv("KEY_HOME", "")
	os.Unsetenv("KEY_HOME")
	t.Setenv("KEY_SHARED", "")
	os.Unsetenv("KEY_SHARED")

	loadDotEnv()

	if got := os.Getenv("KEY_CWD"); got != "from_cwd" {
		t.Errorf("cwd-only key not loaded: KEY_CWD=%q", got)
	}
	if got := os.Getenv("KEY_HOME"); got != "" {
		t.Errorf("~/.env must not be auto-loaded: KEY_HOME=%q", got)
	}
	if got := os.Getenv("KEY_SHARED"); got != "cwd_wins" {
		t.Errorf("cwd .env should load before Maddog credentials: KEY_SHARED=%q want cwd_wins", got)
	}
}

// TestLoadDotEnvReadsGlobalCredentials proves `maddog setup`'s target — the
// Maddog-owned credentials file in the user config dir — is loaded from any
// working directory, while a project ./.env still wins on a shared key.
func TestLoadDotEnvReadsGlobalCredentials(t *testing.T) {
	cwd := t.TempDir()
	cfgHome := t.TempDir()

	t.Chdir(cwd)
	t.Setenv("HOME", cfgHome)
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))

	cred := UserCredentialsPath()
	if cred == "" {
		t.Skip("user config dir unresolved on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(cred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("KEY_GLOBAL=from_credentials\nKEY_SHARED=global_loses\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("KEY_SHARED=cwd_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KEY_GLOBAL", "")
	os.Unsetenv("KEY_GLOBAL")
	t.Setenv("KEY_SHARED", "")
	os.Unsetenv("KEY_SHARED")

	loadDotEnv()

	if got := os.Getenv("KEY_GLOBAL"); got != "from_credentials" {
		t.Errorf("global credentials not loaded: KEY_GLOBAL=%q want from_credentials", got)
	}
	if got := os.Getenv("KEY_SHARED"); got != "cwd_wins" {
		t.Errorf("project .env should win over global credentials: KEY_SHARED=%q want cwd_wins", got)
	}
}

// TestLoadDotEnvDoesNotOverrideEnv confirms an already-set environment variable
// beats both .env files (the documented first-wins contract).
func TestLoadDotEnvDoesNotOverrideEnv(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("PINNED=from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PINNED", "from_env")

	loadDotEnv()

	if got := os.Getenv("PINNED"); got != "from_env" {
		t.Errorf("env var must win over .env: PINNED=%q want from_env", got)
	}
}
