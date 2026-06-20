package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserStatePathsUseMaddogRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("AppData", filepath.Join(root, "AppData"))

	paths := map[string]string{
		"config":      UserConfigPath(),
		"credentials": UserCredentialsPath(),
		"archive":     ArchiveDir(),
		"sessions":    SessionDir(),
		"cache":       CacheDir(),
		"memory":      MemoryUserDir(),
	}
	for name, path := range paths {
		clean := filepath.Clean(path)
		parts := strings.Split(clean, string(filepath.Separator))
		hasMaddog := false
		for _, p := range parts {
			if p == "maddog" {
				hasMaddog = true
			}
			if strings.EqualFold(p, "reasonix") {
				t.Fatalf("%s path still uses reasonix root: %q", name, path)
			}
		}
		if !hasMaddog {
			t.Fatalf("%s path = %q, want path under maddog", name, path)
		}
	}
}

func TestProjectConfigIgnoresReasonixProjectConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte("default_model = \"legacy\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel == "legacy" {
		t.Fatalf("Maddog loaded legacy Reasonix project config")
	}
	if got := SourcePathForRoot(dir); got != "" {
		t.Fatalf("SourcePathForRoot = %q, want no source for legacy-only workspace", got)
	}

	if err := os.WriteFile(filepath.Join(dir, ProjectConfigFilename), []byte("default_model = \"maddog\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "maddog" {
		t.Fatalf("canonical default_model = %q, want maddog", cfg.DefaultModel)
	}
	if got := SourcePathForRoot(dir); got != filepath.Join(dir, ProjectConfigFilename) {
		t.Fatalf("source preferred = %q, want maddog file", got)
	}
}

func TestProjectConfigSourceIgnoresReasonixProjectConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte("default_model = \"legacy\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := projectConfigSourceForRoot(dir); got != filepath.Join(dir, ProjectConfigFilename) {
		t.Fatalf("project source = %q, want canonical Maddog config path", got)
	}
}

func TestCommandDirsPreferMaddogAndIgnoreReasonix(t *testing.T) {
	dirs := CommandDirsForRoot(".")
	joined := strings.Join(dirs, "\n")
	for _, want := range []string{
		filepath.Join(".maddog", "commands"),
		filepath.Join(".claude", "commands"),
		filepath.Join(".agents", "commands"),
		filepath.Join(".agent", "commands"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("CommandDirs missing %q\ngot:\n%s", want, joined)
		}
	}
	if last := dirs[len(dirs)-1]; last != filepath.Join(".maddog", "commands") {
		t.Fatalf("project .maddog/commands should be highest priority, got %q", last)
	}
	if strings.Contains(joined, filepath.Join(".reasonix", "commands")) {
		t.Fatalf("CommandDirs should not read original Reasonix command dirs:\n%s", joined)
	}
}
