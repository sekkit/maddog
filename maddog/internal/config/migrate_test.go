package config

import (
	"os"
	"path/filepath"
	"testing"
)

func isolatedMaddogHome(t *testing.T) (src, dest string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	return filepath.Join(home, ".maddog", "config.json"), userConfigPath()
}

func writeMaddogConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyIfNeededDoesNotImportOriginalMaddogInstall(t *testing.T) {
	src, dest := isolatedMaddogHome(t)
	writeMaddogConfig(t, src, `{"apiKey":"sk-original-maddog","mcpServers":{"original":{"command":"original-bin"}}}`)

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res != nil {
		t.Fatalf("Maddog must not auto-import original Maddog config, got %+v", res)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("Maddog migration wrote user config from original Maddog install, stat err=%v", err)
	}
	if _, err := os.Stat(UserCredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("Maddog migration wrote credentials from original Maddog install, stat err=%v", err)
	}
}
