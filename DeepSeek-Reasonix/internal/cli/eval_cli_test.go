package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	replayeval "reasonix/internal/eval"
)

func TestEvalGuardPromotesCandidateSkill(t *testing.T) {
	replayDir := t.TempDir()
	for i := 0; i < 5; i++ {
		b := replayeval.ReplayBundle{
			SessionID: "s",
			Outcome:   replayeval.OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "done", TotalTurns: 1},
			Timestamp: time.Date(2026, 6, 8, 0, 0, i, 0, time.UTC),
		}
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(replayDir, "bundle-"+string(rune('a'+i))+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	project := t.TempDir()
	candidate := filepath.Join(project, "candidate.md")
	if err := os.WriteFile(candidate, []byte(`---
name: better-docs
description: Better docs
runAs: inline
---

Use replay evidence to improve docs.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rc := evalCommand([]string{"guard", "--dir", replayDir, "--promote-skill", candidate, "--project-root", project})
	if rc != 0 {
		t.Fatalf("eval guard rc = %d, want 0", rc)
	}
	if _, err := os.Stat(filepath.Join(project, ".reasonix", "skills", "better-docs", "SKILL.md")); err != nil {
		t.Fatalf("promoted skill missing: %v", err)
	}
}

func TestEvalGuardRejectsTooFewBundles(t *testing.T) {
	dir := t.TempDir()
	data, err := json.Marshal(replayeval.ReplayBundle{SessionID: "one", Outcome: replayeval.OutcomeInfo{Success: true, GoalMet: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if rc := evalCommand([]string{"guard", "--dir", dir}); rc == 0 {
		t.Fatal("eval guard should reject fewer than the default minimum bundles")
	}
}
