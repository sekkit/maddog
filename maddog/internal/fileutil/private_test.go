package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDirDoesNotRetagCurrentDirectory(t *testing.T) {
	before, err := os.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir("."); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("current directory mode changed from %o to %o", before.Mode().Perm(), after.Mode().Perm())
	}
}

func TestPrivateHelpersRejectFinalSymlink(t *testing.T) {
	realDir := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := EnsurePrivateDir(linkDir); err == nil {
		t.Fatal("EnsurePrivateDir accepted a symlink target")
	}

	realFile := filepath.Join(realDir, "file")
	if err := os.WriteFile(realFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkFile := filepath.Join(t.TempDir(), "file-link")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	if err := ProtectPrivateFile(linkFile); err == nil {
		t.Fatal("ProtectPrivateFile accepted a symlink target")
	}
}
