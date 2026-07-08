package environment

import (
	"os"
	"path/filepath"
	"testing"

	"maddog/internal/config"
)

func TestRefreshMissingWailsWritesRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MADDOG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("PATH", filepath.Join(home, "bin"))
	root := t.TempDir()
	reg, err := Refresh(root)
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if reg.Tools["wails"].Status != ToolStatusMissing {
		t.Fatalf("wails status = %q, want missing", reg.Tools["wails"].Status)
	}
	if _, err := os.Stat(config.ProjectEnvironmentRegistryPath(root)); err != nil {
		t.Fatalf("registry file not written: %v", err)
	}
}

func TestListToolsIncludesCoreResolvers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MADDOG_STATE_HOME", filepath.Join(home, "state"))
	root := t.TempDir()
	tools, err := ListTools(root)
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Record.Name] = true
	}
	for _, want := range []string{"go", "pxpipe", "npx", "pnpm", "wails", "create-dmg", "nfpm", "makensis"} {
		if !got[want] {
			t.Fatalf("missing tool %q in %+v", want, got)
		}
	}
}
