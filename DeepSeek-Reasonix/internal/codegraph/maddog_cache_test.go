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
	t.Setenv("REASONIX_CACHE_DIR", "")
	t.Setenv("MADDOG_CACHE_DIR", "")

	got := CacheDir()
	wantPart := filepath.Join("maddog", "codegraph", Version)
	if !strings.Contains(got, wantPart) {
		t.Fatalf("CacheDir = %q, want namespace %q", got, wantPart)
	}
	if strings.Contains(strings.ToLower(got), "reasonix") {
		t.Fatalf("CacheDir still contains reasonix: %q", got)
	}
}

func TestCacheDirIgnoresReasonixEnv(t *testing.T) {
	legacy := t.TempDir()
	t.Setenv("MADDOG_CACHE_DIR", "")
	t.Setenv("REASONIX_CACHE_DIR", legacy)
	if got := CacheDir(); strings.HasPrefix(got, legacy) {
		t.Fatalf("CacheDir should ignore REASONIX_CACHE_DIR, got %q", got)
	}

	canonical := t.TempDir()
	t.Setenv("MADDOG_CACHE_DIR", canonical)
	if got, want := CacheDir(), filepath.Join(canonical, "codegraph", Version); got != want {
		t.Fatalf("canonical CacheDir = %q, want %q", got, want)
	}
}
