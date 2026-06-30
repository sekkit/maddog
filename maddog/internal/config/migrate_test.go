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

func TestMigrateSupportData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping since legacyOSSupportDir equals current maddogHomeDir on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MADDOG_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	legacyConf := legacyUserConfigPath()
	if legacyConf == "" {
		t.Skip("skipping because legacy config path is empty")
	}
	legacyDir := filepath.Dir(legacyConf)

	// Write data to the legacy support directory
	filesToWrite := map[string]string{
		"config.toml":                  "language = \"zh\"",
		"hooks.json":                   `{"hook":"test"}`,
		"sessions/s1.json":             `{"id":"s1"}`,
		"projects/p1/sessions/s2.json": `{"id":"s2"}`,
		"skills/custom.md":             `custom skill`,
		"archive/a1.json":              `{"compacted": true}`,
	}
	for rel, content := range filesToWrite {
		path := filepath.Join(legacyDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(legacyDir, "sessions"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(legacyDir, "sessions", "s1.json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(legacyDir, "hooks.json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	res, err := MigrateLegacyIfNeeded()
	if err != nil {
		t.Fatalf("MigrateLegacyIfNeeded failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected migration result, got nil")
	}

	newDir := filepath.Dir(userConfigPath())
	for rel, expectedContent := range filesToWrite {
		if rel == "config.toml" {
			continue
		}
		newPath := filepath.Join(newDir, rel)
		data, err := os.ReadFile(newPath)
		if err != nil {
			t.Errorf("expected file %s to be migrated, but got error: %v", rel, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("file %s content mismatch: got %q, want %q", rel, string(data), expectedContent)
		}
	}
	if runtime.GOOS != "windows" {
		for _, check := range []struct {
			rel  string
			perm os.FileMode
		}{
			{rel: "sessions", perm: 0o700},
			{rel: "sessions/s1.json", perm: 0o600},
			{rel: "hooks.json", perm: 0o600},
		} {
			info, err := os.Stat(filepath.Join(newDir, check.rel))
			if err != nil {
				t.Fatalf("stat migrated %s: %v", check.rel, err)
			}
			if got := info.Mode().Perm(); got != check.perm {
				t.Fatalf("migrated %s mode = %o, want %o", check.rel, got, check.perm)
			}
		}
	}
}
