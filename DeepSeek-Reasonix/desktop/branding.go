package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/boot"
)

const (
	desktopAppTitle       = "Maddog Dev"
	desktopStateDirName   = "maddog-dev"
	desktopSingleInstance = "com.maddog.desktop"
)

// singleInstanceID is used by Wails to route a second desktop launch back to the
// running instance. Keep it separate from Reasonix so both apps can run side by
// side during development.
const singleInstanceID = desktopSingleInstance

func desktopConfigDir() string {
	dir, err := os.UserConfigDir()
	if err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, desktopStateDirName)
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "."+desktopStateDirName)
	}
	return ""
}

func desktopSessionDir() string {
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
	opts.SessionDir = desktopSessionDir()
	opts.ArchiveDir = desktopArchiveDir()
	opts.MemoryUserDir = desktopMemoryUserDir()
	return opts
}
