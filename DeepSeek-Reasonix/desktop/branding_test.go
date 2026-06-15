package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/codegraph"
	"reasonix/internal/config"
	"reasonix/internal/hook"
)

func TestWailsDevBinaryNameIsMaddog(t *testing.T) {
	body, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatalf("read wails.json: %v", err)
	}
	var cfg struct {
		Name           string `json:"name"`
		OutputFilename string `json:"outputfilename"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse wails.json: %v", err)
	}

	if cfg.OutputFilename != "maddog" {
		t.Fatalf("outputfilename = %q, want maddog so wails dev emits maddog-dev.exe", cfg.OutputFilename)
	}
	if got := cfg.OutputFilename + "-dev.exe"; got != "maddog-dev.exe" {
		t.Fatalf("derived dev exe = %q, want maddog-dev.exe", got)
	}
	if strings.Contains(strings.ToLower(cfg.Name), "reasonix") {
		t.Fatalf("wails app name should not contain reasonix: %q", cfg.Name)
	}
}

func TestDesktopIdentityUsesMaddogNamespace(t *testing.T) {
	if singleInstanceID == "com.reasonix.desktop" || strings.Contains(strings.ToLower(singleInstanceID), "reasonix") {
		t.Fatalf("singleInstanceID = %q, want Maddog-specific namespace", singleInstanceID)
	}
	if !strings.Contains(strings.ToLower(singleInstanceID), "maddog") {
		t.Fatalf("singleInstanceID = %q, want Maddog namespace", singleInstanceID)
	}
}

func TestDesktopStatePathsUseMaddogDevRoot(t *testing.T) {
	isolateDesktopUserDirs(t)

	paths := map[string]string{
		"desktopConfigDir":     desktopConfigDir(),
		"desktopSessionDir":    desktopSessionDir(),
		"desktopArchiveDir":    desktopArchiveDir(),
		"desktopMemoryUserDir": desktopMemoryUserDir(),
		"workspaceStatePath":   workspaceStatePath(),
		"workspaceListPath":    workspaceListPath(),
		"windowStatePath":      windowStatePath(),
		"globalWorkspaceRoot":  globalWorkspaceRoot(),
		"globalTopicTitles":    topicTitlesPath(""),
		"globalTopicSources":   topicTitleSourcesPath(""),
	}
	for name, path := range paths {
		if strings.TrimSpace(path) == "" {
			t.Fatalf("%s is empty", name)
		}
		cleanParts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
		hasMaddogDev := false
		hasReasonixRoot := false
		for _, part := range cleanParts {
			switch strings.ToLower(part) {
			case "maddog-dev":
				hasMaddogDev = true
			case "reasonix":
				hasReasonixRoot = true
			}
		}
		if !hasMaddogDev {
			t.Fatalf("%s = %q, want path under maddog-dev", name, path)
		}
		if hasReasonixRoot {
			t.Fatalf("%s = %q, should not use the reasonix desktop state root", name, path)
		}
	}
}

func TestDesktopConfigBrandingUsesMaddogFiles(t *testing.T) {
	isolateDesktopUserDirs(t)

	if got := config.ProjectConfigFile(); got != "maddog.toml" {
		t.Fatalf("ProjectConfigFile = %q, want maddog.toml", got)
	}
	if got := config.ProjectStateDir(); got != ".maddog" {
		t.Fatalf("ProjectStateDir = %q, want .maddog", got)
	}
	if got := config.UserStateDir(); got != "maddog-dev" {
		t.Fatalf("UserStateDir = %q, want maddog-dev", got)
	}
	if got := projectConfigPathForRoot("/tmp/workspace"); got != filepath.Join("/tmp/workspace", "maddog.toml") {
		t.Fatalf("projectConfigPathForRoot = %q, want maddog.toml", got)
	}
	if got := topicTitlesPath("/tmp/workspace"); strings.Contains(got, ".reasonix") || !strings.Contains(got, ".maddog") {
		t.Fatalf("topicTitlesPath = %q, want .maddog and no .reasonix", got)
	}
	if got := hook.ProjectSettingsPath("/tmp/workspace"); strings.Contains(got, ".reasonix") || !strings.Contains(got, ".maddog") {
		t.Fatalf("ProjectSettingsPath = %q, want .maddog and no .reasonix", got)
	}
	if got := hook.TrustPath("/tmp/home"); strings.Contains(got, ".reasonix") || !strings.Contains(got, ".maddog") {
		t.Fatalf("TrustPath = %q, want .maddog and no .reasonix", got)
	}
}

func TestDesktopCacheUsesMaddogNamespaceAcrossPlatforms(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	reasonixCache := filepath.Join(home, "reasonix-cache")
	maddogCache := filepath.Join(home, "maddog-cache")
	t.Setenv("REASONIX_CACHE_DIR", reasonixCache)

	if got := codegraph.CacheDir(); strings.HasPrefix(got, reasonixCache) {
		t.Fatalf("CacheDir = %q, should ignore REASONIX_CACHE_DIR in Maddog", got)
	}
	if !strings.Contains(filepath.ToSlash(codegraph.CacheDir()), "maddog-dev/codegraph") {
		t.Fatalf("CacheDir = %q, want maddog-dev codegraph cache", codegraph.CacheDir())
	}

	t.Setenv("MADDOG_CACHE_DIR", maddogCache)
	if got := codegraph.CacheDir(); !strings.HasPrefix(got, maddogCache) {
		t.Fatalf("CacheDir = %q, want MADDOG_CACHE_DIR prefix %q", got, maddogCache)
	}
}

func TestListSessionsDoesNotReadReasonixSessionRoot(t *testing.T) {
	isolateDesktopUserDirs(t)

	reasonixDir := filepath.Join(filepath.Dir(desktopConfigDir()), "reasonix", "sessions")
	if err := os.MkdirAll(reasonixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reasonixSession := filepath.Join(reasonixDir, "reasonix-session.jsonl")
	if err := os.WriteFile(reasonixSession, []byte(`{"role":"user","content":"from reasonix"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := NewApp().ListSessions()
	if len(got) != 0 {
		t.Fatalf("ListSessions read Reasonix sessions: %#v", got)
	}
}
