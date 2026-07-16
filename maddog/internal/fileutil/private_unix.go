//go:build !windows

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsurePrivateDir creates dir and tightens an existing directory to owner-only.
func EnsurePrivateDir(path string) error {
	if err := rejectPrivateSymlink(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	clean := filepath.Clean(path)
	if clean == "." || filepath.Dir(clean) == clean {
		return nil
	}
	if err := rejectPrivateSymlink(path); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

// ProtectPrivateFile tightens an existing file to owner read/write only.
func ProtectPrivateFile(path string) error {
	if err := rejectPrivateSymlink(path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func rejectPrivateSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private path must not be a symlink: %s", path)
	}
	return nil
}
