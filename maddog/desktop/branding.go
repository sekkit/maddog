package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"maddog/internal/boot"
	"maddog/internal/config"
)

const (
	desktopAppTitle           = "Maddog"
	desktopStateDirName       = "desktop"
	legacyDesktopStateDirName = "maddog-dev"
	desktopSingleInstance     = "com.maddog.desktop"
)

var migratedDesktopState sync.Map

// singleInstanceID is used by Wails to route a second desktop launch back to the
// running instance under one stable Maddog desktop identity.
const singleInstanceID = desktopSingleInstance

func desktopConfigDir() string {
	home := config.MaddogHomeDir()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	dir := filepath.Join(home, desktopStateDirName)
	for _, legacy := range legacyDesktopConfigDirs(home) {
		migrateLegacyDesktopState(legacy, dir)
	}
	return dir
}

func legacyDesktopConfigDirs(home string) []string {
	parent := filepath.Dir(home)
	candidates := []string{
		filepath.Join(parent, legacyDesktopStateDirName),
		filepath.Join(parent, "."+legacyDesktopStateDirName),
	}
	if root, err := os.UserConfigDir(); err == nil && strings.TrimSpace(root) != "" {
		candidates = append(candidates, filepath.Join(root, legacyDesktopStateDirName))
	}
	if userHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(userHome) != "" {
		candidates = append(candidates, filepath.Join(userHome, "."+legacyDesktopStateDirName))
	}
	out := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || sameDesktopDir(candidate, home) {
			continue
		}
		key := strings.ToLower(filepath.Clean(candidate))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

func migrateLegacyDesktopState(legacyDir, currentDir string) {
	if strings.TrimSpace(legacyDir) == "" || strings.TrimSpace(currentDir) == "" {
		return
	}
	if sameDesktopDir(legacyDir, currentDir) {
		return
	}
	info, err := os.Stat(legacyDir)
	if err != nil || !info.IsDir() {
		return
	}
	if currentInfo, err := os.Stat(currentDir); err == nil && !currentInfo.IsDir() {
		return
	}
	migrationKey := filepath.Clean(legacyDir) + "\x00" + filepath.Clean(currentDir)
	if _, loaded := migratedDesktopState.LoadOrStore(migrationKey, struct{}{}); loaded {
		return
	}
	_ = filepath.WalkDir(legacyDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(legacyDir, path)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(currentDir, rel)
		if d.IsDir() {
			_ = os.MkdirAll(target, 0o755)
			return nil
		}
		if _, err := os.Stat(target); err == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil
		}
		copyDesktopStateFile(path, target)
		return nil
	})
}

func copyDesktopStateFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return
	}
	defer out.Close()
	_, _ = io.Copy(out, in)
}

func desktopSessionDir(workspaceRoot ...string) string {
	if len(workspaceRoot) > 0 {
		root := strings.TrimSpace(workspaceRoot[0])
		if root != "" && !sameDesktopDir(root, globalWorkspaceRoot()) {
			if dir := config.ProjectSessionDir(root); dir != "" {
				return dir
			}
		}
	}
	return desktopStatePath("sessions")
}

func desktopProjectSessionDir(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root != "" && !sameDesktopDir(root, globalWorkspaceRoot()) {
		if dir := config.ProjectSessionDir(root); dir != "" {
			return dir
		}
	}
	return desktopStatePath("sessions")
}

func desktopArchiveDir() string {
	return desktopStatePath("archive")
}

func desktopMemoryUserDir() string {
	return desktopConfigDir()
}

func desktopStatePath(elem ...string) string {
	dir := desktopConfigDir()
	if dir == "" {
		return ""
	}
	parts := append([]string{dir}, elem...)
	return filepath.Join(parts...)
}

func desktopBootOptions(opts boot.Options) boot.Options {
	if strings.TrimSpace(opts.SessionDir) == "" {
		opts.SessionDir = desktopSessionDir(opts.WorkspaceRoot)
	}
	opts.ArchiveDir = desktopArchiveDir()
	opts.MemoryUserDir = desktopMemoryUserDir()
	return opts
}
