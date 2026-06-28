package codegraph

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheDirUsesMaddogNamespace(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	t.Setenv("LocalAppData", base)
	t.Setenv("MADDOG_CACHE_DIR", "")
	t.Setenv("MADDOG_CACHE_DIR", "")

	got := CacheDir()
	wantPart := filepath.Join("maddog", "codegraph", Version)
	if !strings.Contains(got, wantPart) {
		t.Fatalf("CacheDir = %q, want namespace %q", got, wantPart)
	}
	if !strings.Contains(strings.ToLower(got), "maddog") {
		t.Fatalf("CacheDir should contain maddog namespace: %q", got)
	}
}

func TestCacheDirUsesMaddogEnv(t *testing.T) {
	canonical := t.TempDir()
	t.Setenv("MADDOG_CACHE_DIR", canonical)
	if got, want := CacheDir(), filepath.Join(canonical, "codegraph", Version); got != want {
		t.Fatalf("canonical CacheDir = %q, want %q", got, want)
	}
}
