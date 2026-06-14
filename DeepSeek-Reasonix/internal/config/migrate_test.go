package config

import (
	"os"
	"path/filepath"
	"testing"
)

func isolatedReasonixHome(t *testing.T) (src, dest string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	return filepath.Join(home, ".reasonix", "config.json"), userConfigPath()
}

func writeReasonixConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyIfNeededDoesNotImportOriginalReasonixInstall(t *testing.T) {
	src, dest := isolatedReasonixHome(t)
	writeReasonixConfig(t, src, `{"apiKey":"sk-original-reasonix","mcpServers":{"original":{"command":"original-bin"}}}`)

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res != nil {
		t.Fatalf("Maddog must not auto-import original Reasonix config, got %+v", res)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("Maddog migration wrote user config from original Reasonix install, stat err=%v", err)
	}
	if _, err := os.Stat(UserCredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("Maddog migration wrote credentials from original Reasonix install, stat err=%v", err)
	}
}
