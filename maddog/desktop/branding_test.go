package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	if cfg.Name != "maddog-dev" {
		t.Fatalf("name = %q, want maddog-dev", cfg.Name)
	}
	if cfg.OutputFilename != "maddog-dev" {
		t.Fatalf("outputfilename = %q, want maddog-dev so Wails emits maddog-dev.exe", cfg.OutputFilename)
	}
	if got := cfg.OutputFilename + ".exe"; got != "maddog-dev.exe" {
		t.Fatalf("derived desktop exe = %q, want maddog-dev.exe", got)
	}
}

func TestDesktopIdentityUsesMaddogNamespace(t *testing.T) {
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
		hasMaddogRoot := false
		for _, part := range cleanParts {
			switch strings.ToLower(part) {
			case "maddog-dev":
				hasMaddogDev = true
			case "maddog":
				hasMaddogRoot = true
			}
		}
		if !hasMaddogDev {
			t.Fatalf("%s = %q, want path under maddog-dev", name, path)
		}
		if hasMaddogRoot {
			t.Fatalf("%s = %q, should not use the maddog desktop state root", name, path)
		}
	}
}

func TestListSessionsDoesNotReadMaddogSessionRoot(t *testing.T) {
	isolateDesktopUserDirs(t)

	maddogDir := filepath.Join(filepath.Dir(desktopConfigDir()), "maddog", "sessions")
	if err := os.MkdirAll(maddogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	maddogSession := filepath.Join(maddogDir, "maddog-session.jsonl")
	if err := os.WriteFile(maddogSession, []byte(`{"role":"user","content":"from maddog"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := NewApp().ListSessions()
	if len(got) != 0 {
		t.Fatalf("ListSessions read Maddog sessions: %#v", got)
	}
}
