package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/config"
)

func TestWailsReleaseBinaryNameIsMaddog(t *testing.T) {
	body, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatalf("read wails.json: %v", err)
	}
	var cfg struct {
		Name           string `json:"name"`
		OutputFilename string `json:"outputfilename"`
		Info           struct {
			ProductName string `json:"productName"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse wails.json: %v", err)
	}

	if cfg.Name != "maddog" {
		t.Fatalf("name = %q, want maddog", cfg.Name)
	}
	if cfg.OutputFilename != "maddog" {
		t.Fatalf("outputfilename = %q, want maddog so Wails emits maddog.exe", cfg.OutputFilename)
	}
	if got := cfg.OutputFilename + ".exe"; got != "maddog.exe" {
		t.Fatalf("derived desktop exe = %q, want maddog.exe", got)
	}
	if cfg.Info.ProductName != "Maddog" {
		t.Fatalf("productName = %q, want Maddog", cfg.Info.ProductName)
	}
}

func TestDesktopIdentityUsesMaddogNamespace(t *testing.T) {
	if !strings.Contains(strings.ToLower(singleInstanceID), "maddog") {
		t.Fatalf("singleInstanceID = %q, want Maddog namespace", singleInstanceID)
	}
}

func TestDesktopStatePathsUseMaddogRoot(t *testing.T) {
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
		hasMaddogRoot := false
		hasDesktopRoot := false
		for _, part := range cleanParts {
			switch strings.ToLower(part) {
			case "maddog-dev":
				t.Fatalf("%s = %q, should not use the legacy maddog-dev desktop state root", name, path)
			case "maddog":
				hasMaddogRoot = true
			case "desktop":
				hasDesktopRoot = true
			}
		}
		if !hasMaddogRoot {
			t.Fatalf("%s = %q, want path under maddog", name, path)
		}
		if !hasDesktopRoot {
			t.Fatalf("%s = %q, want path under maddog desktop state subdirectory", name, path)
		}
	}
}

func TestDesktopStateMigratesLegacyMaddogDevWithoutOverwritingStableFiles(t *testing.T) {
	isolateDesktopUserDirs(t)

	maddogHome := config.MaddogHomeDir()
	legacy := filepath.Join(filepath.Dir(maddogHome), legacyDesktopStateDirName)
	current := filepath.Join(maddogHome, desktopStateDirName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}

	legacyWindow := `{"width":1024,"height":768,"x":25,"y":35,"maximised":true}`
	if err := os.WriteFile(filepath.Join(legacy, "desktop-window.json"), []byte(legacyWindow), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "desktop-workspace"), []byte(`C:\legacy-workspace`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "desktop-workspace"), []byte(`C:\stable-workspace`), 0o644); err != nil {
		t.Fatal(err)
	}

	state, ok := loadWindowState()
	if !ok {
		t.Fatal("loadWindowState did not migrate legacy window state")
	}
	if state.Width != 1024 || state.Height != 768 || state.X != 25 || state.Y != 35 || !state.Maximised {
		t.Fatalf("migrated window state = %#v", state)
	}
	if got := loadWorkspace(); got != `C:\stable-workspace` {
		t.Fatalf("loadWorkspace = %q, want existing stable file to win", got)
	}
	if _, err := os.Stat(filepath.Join(current, "desktop-window.json")); err != nil {
		t.Fatalf("legacy window state was not copied to stable root: %v", err)
	}
}

func TestListSessionsDoesNotReadMaddogHomeSessionRoot(t *testing.T) {
	isolateDesktopUserDirs(t)

	rootSessionDir := filepath.Join(config.MaddogHomeDir(), "sessions")
	if err := os.MkdirAll(rootSessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootSession := filepath.Join(rootSessionDir, "maddog-session.jsonl")
	if err := os.WriteFile(rootSession, []byte(`{"role":"user","content":"from root"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := NewApp().ListSessions()
	if len(got) != 0 {
		t.Fatalf("ListSessions read Maddog home sessions outside desktop state root: %#v", got)
	}
}
