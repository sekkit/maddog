package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDefaultBrandingUsesReasonixPaths(t *testing.T) {
	if ProjectConfigFile() != "reasonix.toml" {
		t.Fatalf("ProjectConfigFile = %q, want reasonix.toml", ProjectConfigFile())
	}
	if ProjectStateDir() != ".reasonix" {
		t.Fatalf("ProjectStateDir = %q, want .reasonix", ProjectStateDir())
	}
	if UserStateDir() != "reasonix" {
		t.Fatalf("UserStateDir = %q, want reasonix", UserStateDir())
	}
}

func TestConfiguredBrandingUsesOwnProjectAndUserPaths(t *testing.T) {
	old := ActiveBranding()
	ConfigureBranding(Branding{
		ProjectConfigFile: "maddog.toml",
		UserStateDir:      "maddog-dev",
		ProjectStateDir:   ".maddog",
		EnvPrefix:         "MADDOG",
	})
	t.Cleanup(func() { ConfigureBranding(old) })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "LocalAppData"))
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("default_model = \"deepseek-pro\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "maddog.toml"), []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "deepseek-flash" {
		t.Fatalf("DefaultModel = %q, want maddog.toml value", cfg.DefaultModel)
	}
	if got := SourcePathForRoot(project); got != filepath.Join(project, "maddog.toml") {
		t.Fatalf("SourcePathForRoot = %q, want project maddog.toml", got)
	}
	if strings.Contains(UserConfigPath(), "reasonix") || !strings.Contains(UserConfigPath(), "maddog-dev") {
		t.Fatalf("UserConfigPath = %q, want maddog-dev and no reasonix", UserConfigPath())
	}

	dirs := ProjectConventionDirs()
	if !slices.Contains(dirs, ".maddog") {
		t.Fatalf("ProjectConventionDirs = %#v, want .maddog", dirs)
	}
	if slices.Contains(dirs, ".reasonix") {
		t.Fatalf("ProjectConventionDirs = %#v, should not include .reasonix for Maddog branding", dirs)
	}
}
