package skilleval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/evidence"
	"maddog/internal/provider"
)

func TestBuildBundleV2FromSessionEvidenceAndHistory(t *testing.T) {
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "fix parser"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "used parser skill"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "parser fixed"})
	dir := t.TempDir()
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)

	bundle, path, err := CaptureBundle(CaptureOptions{
		SessionID: "sess/123",
		Task:      "fix parser",
		SkillName: "parser-helper",
		Session:   sess,
		Evidence: []evidence.Receipt{
			{ToolName: "read_file", Success: true, Paths: []string{"parser.go"}, Read: true},
			{ToolName: "go test", Success: false, Command: "go test ./..."},
		},
		History: []HistoryItem{
			{Kind: "skill_invoked", Text: "parser-helper"},
			{Kind: "checkpoint", Text: "before edit"},
		},
		Dir: dir,
		Now: now,
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if bundle.Version != 2 || bundle.SessionID != "sess/123" || bundle.ID == "" {
		t.Fatalf("bundle identity = %+v, want v2 with session id", bundle)
	}
	if bundle.Outcome.FinalAnswer != "parser fixed" || bundle.Outcome.TotalTurns != 1 || bundle.Outcome.ToolErrors != 1 {
		t.Fatalf("bundle outcome = %+v", bundle.Outcome)
	}
	if len(bundle.Messages) != 3 || len(bundle.Evidence) != 2 || len(bundle.History) != 2 {
		t.Fatalf("bundle snapshot counts = messages %d evidence %d history %d", len(bundle.Messages), len(bundle.Evidence), len(bundle.History))
	}
	if filepath.Base(path) != "sess-123.json" {
		t.Fatalf("bundle path = %q, want sanitized session filename", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("bundle file is not valid JSON: %s", raw)
	}
	loaded, err := LoadBundle(path)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if loaded.ID != bundle.ID || loaded.CreatedAt != now.UTC() || loaded.History[0].Kind != "skill_invoked" {
		t.Fatalf("loaded bundle = %+v, want roundtrip", loaded)
	}
}

func TestCaptureBundleDoesNotOverwriteSameSessionHistory(t *testing.T) {
	dir := t.TempDir()
	first, firstPath, err := CaptureBundle(CaptureOptions{
		SessionID: "sess/123",
		Messages:  []provider.Message{{Role: provider.RoleAssistant, Content: "first"}},
		Dir:       dir,
		Now:       time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle first: %v", err)
	}
	second, secondPath, err := CaptureBundle(CaptureOptions{
		SessionID: "sess/123",
		Messages:  []provider.Message{{Role: provider.RoleAssistant, Content: "second"}},
		Dir:       dir,
		Now:       time.Date(2026, 6, 30, 9, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle second: %v", err)
	}
	if firstPath == secondPath {
		t.Fatalf("second capture reused %q, want a distinct replay file", firstPath)
	}
	loadedFirst, err := LoadBundle(firstPath)
	if err != nil {
		t.Fatalf("LoadBundle first: %v", err)
	}
	if loadedFirst.ID != first.ID || loadedFirst.Outcome.FinalAnswer != "first" {
		t.Fatalf("first bundle was overwritten: %+v", loadedFirst)
	}
	loadedSecond, err := LoadBundle(secondPath)
	if err != nil {
		t.Fatalf("LoadBundle second: %v", err)
	}
	if loadedSecond.ID != second.ID || loadedSecond.Outcome.FinalAnswer != "second" {
		t.Fatalf("second bundle mismatch: %+v", loadedSecond)
	}
}
