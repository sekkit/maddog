package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"maddog/internal/repair"
)

func TestDesktopPathPrefersPackagedSibling(t *testing.T) {
	dir := t.TempDir()
	guard := filepath.Join(dir, "maddog")
	name := desktopName
	if runtime.GOOS == "windows" {
		guard += ".exe"
		name += ".exe"
	}
	want := filepath.Join(dir, name)
	if err := os.WriteFile(want, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := desktopPath(guard); got != want {
		t.Fatalf("desktopPath = %q, want %q", got, want)
	}
}

func TestDesktopPathDebEntryPointDoesNotLaunchItself(t *testing.T) {
	guard := filepath.Join(string(filepath.Separator), "usr", "bin", "maddog-desktop")
	want := filepath.Join(string(filepath.Separator), "usr", "lib", "maddog", "maddog-desktop")
	if got := desktopPathForOS(guard, "linux"); got != want {
		t.Fatalf("desktopPathForOS = %q, want %q", got, want)
	}
}

func TestOnlyPrimaryGuardHandlesRelaunchExit(t *testing.T) {
	if !shouldRelaunch(true, repair.GuardRelaunchExitCode) {
		t.Fatal("primary guard should handle the relaunch exit code")
	}
	if shouldRelaunch(false, repair.GuardRelaunchExitCode) {
		t.Fatal("secondary guard must not relaunch a payload")
	}
	if shouldRelaunch(true, 1) {
		t.Fatal("ordinary failures must not trigger relaunch")
	}
}
