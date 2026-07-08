package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MigrationResult summarizes a legacy import for callers compiled against the
// previous migration API. MigrateLegacyIfNeeded now returns nil.
type MigrationResult struct {
	From     string
	To       string
	KeyToEnv bool
	Plugins  int
	Warnings []string
}

// MCPGlobalMigrationResult summarizes the v1.9.1 MCP backfill that lifts MCP
// servers from legacy and project-local sources into the user-global config.
type MCPGlobalMigrationResult struct {
	To      string
	Added   int
	Sources int
}

func (r *MigrationResult) Notice() string {
	var b strings.Builder
	fmt.Fprintf(&b, "migrated your previous configuration: %s -> %s", r.From, r.To)
	if r.Plugins > 0 {
		fmt.Fprintf(&b, " (%d MCP server(s))", r.Plugins)
	}
	if r.KeyToEnv {
		b.WriteString("; API key saved to Maddog's credentials store")
	}
	b.WriteString(". The old files were left untouched.")
	for _, w := range r.Warnings {
		b.WriteString("\n  note: " + w)
	}
	return b.String()
}

// MigrateLegacyIfNeeded imports non-config support data from the old OS support
// directory into Maddog's current home. It intentionally does not import
// legacy config.json/config.toml contents, so a separate DeepSeek Maddog install
// cannot silently alter this Maddog's provider configuration.
func MigrateLegacyIfNeeded() (*MigrationResult, error) {
	dest := userConfigDir()
	src := legacyOSSupportDir()
	if dest == "" || src == "" {
		return nil, nil
	}
	if samePath(src, dest) {
		return nil, nil
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	copied, warnings, err := migrateSupportData(src, dest)
	if err != nil {
		return nil, err
	}
	if copied == 0 {
		return nil, nil
	}
	return &MigrationResult{From: src, To: dest, Warnings: warnings}, nil
}

func migrateSupportData(src, dest string) (int, []string, error) {
	var copied int
	var warnings []string
	roots := []string{"hooks.json", "sessions", "projects", "skills", "archive"}
	for _, rel := range roots {
		n, err := copySupportPath(filepath.Join(src, rel), filepath.Join(dest, rel))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		copied += n
	}
	return copied, warnings, nil
}

func copySupportPath(src, dest string) (int, error) {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		if _, err := os.Stat(dest); err == nil {
			return 0, nil
		}
		if err := copyFile(src, dest, info.Mode().Perm()); err != nil {
			return 0, err
		}
		return 1, nil
	}
	copied := 0
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if _, err := os.Stat(target); err == nil {
			return nil
		}
		if err := copyFile(path, target, info.Mode().Perm()); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func copyFile(src, dest string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dest)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	ok = true
	return nil
}
