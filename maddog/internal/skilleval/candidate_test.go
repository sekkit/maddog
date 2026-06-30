package skilleval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/skill"
)

func TestCandidateStoreCreatesPendingAndDedupesByContent(t *testing.T) {
	store := NewCandidateStore(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC) }
	sk := validSkill("parser-helper")

	first, err := store.Create(sk, BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create(sk, BundleV2{ID: "bundle-b"}, "fix parser again")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.Hash == "" || first.Hash != second.Hash {
		t.Fatalf("candidate hashes = %q/%q, want content dedupe", first.Hash, second.Hash)
	}
	if first.Status != CandidatePending || second.Status != CandidatePending {
		t.Fatalf("candidate statuses = %s/%s, want pending", first.Status, second.Status)
	}
	if second.SourceBundleID != "bundle-a" {
		t.Fatalf("deduped candidate source bundle = %q, want original bundle", second.SourceBundleID)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "candidates", first.Hash+".json")); err != nil {
		t.Fatalf("candidate file missing: %v", err)
	}
}

func TestDuplicateCandidateRevalidatesTaskRisk(t *testing.T) {
	store := NewCandidateStore(t.TempDir())
	sk := validSkill("parser-helper")
	first, err := store.Create(sk, BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	if first.Status != CandidatePending {
		t.Fatalf("first status = %s, want pending", first.Status)
	}
	duplicate, err := store.Create(sk, BundleV2{ID: "bundle-b"}, "execute DELETE FROM users")
	if err != nil {
		t.Fatalf("Create duplicate high-risk: %v", err)
	}
	if duplicate.Hash != first.Hash {
		t.Fatalf("duplicate hash = %q, want original %q", duplicate.Hash, first.Hash)
	}
	if duplicate.Status != CandidateRejected || !strings.Contains(duplicate.ValidationReason, "high risk") {
		t.Fatalf("duplicate candidate = %+v, want high-risk rejection", duplicate)
	}
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: t.TempDir(), DisableBuiltins: true})
	if _, _, err := store.Promote(first.Hash, activeStore, skill.ScopeProject); err == nil {
		t.Fatal("Promote after high-risk duplicate succeeded, want lifecycle guard")
	}
}

func TestRejectedCandidateCannotPromote(t *testing.T) {
	store := NewCandidateStore(t.TempDir())
	candidate, err := store.Create(skill.Skill{Name: "bad", Description: "bad"}, BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create invalid: %v", err)
	}
	if candidate.Status != CandidateRejected || !strings.Contains(candidate.ValidationReason, "missing skill body") {
		t.Fatalf("invalid candidate = %+v, want rejected with validation reason", candidate)
	}
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: t.TempDir(), DisableBuiltins: true})
	if _, _, err := store.Promote(candidate.Hash, activeStore, skill.ScopeProject); err == nil {
		t.Fatal("Promote rejected candidate succeeded, want error")
	}
}

func TestPendingCandidateRequiresPassingEvaluationBeforePromote(t *testing.T) {
	store := NewCandidateStore(t.TempDir())
	candidate, err := store.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: t.TempDir(), DisableBuiltins: true})
	if _, _, err := store.Promote(candidate.Hash, activeStore, skill.ScopeProject); err == nil {
		t.Fatal("Promote unevaluated candidate succeeded, want replay evaluation guard")
	}
	if _, err := store.RecordEvaluation(candidate.Hash, ScoreResult{Score: 0.8, Reason: "ok"}, GuardrailResult{Pass: false, Reason: "regression"}); err != nil {
		t.Fatalf("RecordEvaluation failed guardrail: %v", err)
	}
	if _, _, err := store.Promote(candidate.Hash, activeStore, skill.ScopeProject); err == nil {
		t.Fatal("Promote failed guardrail candidate succeeded")
	}
}

func TestPromoteRejectsTamperedCandidateAfterEvaluation(t *testing.T) {
	store := NewCandidateStore(t.TempDir())
	candidate, err := store.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.RecordEvaluation(candidate.Hash, ScoreResult{Score: 0.9, Reason: "ok"}, GuardrailResult{Pass: true, Reason: "passed"}); err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Dir, "candidates", candidate.Hash+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var tampered Candidate
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Skill.Body = "different body after eval"
	if err := writeJSON(filepath.Join(store.Dir, "candidates", candidate.Hash+".json"), tampered); err != nil {
		t.Fatal(err)
	}
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: t.TempDir(), DisableBuiltins: true})
	if _, _, err := store.Promote(candidate.Hash, activeStore, skill.ScopeProject); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("Promote tampered err = %v, want hash mismatch", err)
	}
}

func TestPromoteCandidateWritesActiveSkillAndTransitions(t *testing.T) {
	candidateStore := NewCandidateStore(t.TempDir())
	candidate, err := candidateStore.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	candidate, err = candidateStore.RecordEvaluation(candidate.Hash, ScoreResult{Score: 0.9, Reason: "ok"}, GuardrailResult{Pass: true, Reason: "passed"})
	if err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	projectRoot := t.TempDir()
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: projectRoot, DisableBuiltins: true})

	updated, path, err := candidateStore.Promote(candidate.Hash, activeStore, skill.ScopeProject)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if updated.Status != CandidatePromoted || updated.PromotedPath != path {
		t.Fatalf("promoted candidate = %+v path=%q", updated, path)
	}
	if !strings.HasPrefix(path, filepath.Join(projectRoot, ".maddog", "skills")) {
		t.Fatalf("promoted path = %q, want project skill root", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Use the parser checklist") {
		t.Fatalf("promoted skill content missing body: %s", raw)
	}
	if _, _, err := candidateStore.Promote(candidate.Hash, activeStore, skill.ScopeProject); err == nil {
		t.Fatal("second promotion succeeded, want lifecycle guard")
	}
}

func TestListSkipsInvalidOrTamperedCandidateFiles(t *testing.T) {
	store := NewCandidateStore(t.TempDir())
	candidate, err := store.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "candidates", "not-a-hash.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Dir, "candidates", candidate.Hash+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var tampered Candidate
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Skill.Body = "tampered body"
	if err := writeJSON(filepath.Join(store.Dir, "candidates", candidate.Hash+".json"), tampered); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List returned tampered candidates: %+v", got)
	}
	if _, err := store.Reject("../"+candidate.Hash, "bad"); err == nil {
		t.Fatal("Reject accepted traversal hash")
	}
}

func TestFailedPromotionRestoresPending(t *testing.T) {
	candidateStore := NewCandidateStore(t.TempDir())
	candidate, err := candidateStore.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := candidateStore.RecordEvaluation(candidate.Hash, ScoreResult{Score: 0.9, Reason: "ok"}, GuardrailResult{Pass: true, Reason: "passed"}); err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	projectRoot := t.TempDir()
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: projectRoot, DisableBuiltins: true})
	if _, err := activeStore.CreateWithContent("parser-helper", skill.ScopeProject, "---\nname: parser-helper\ndescription: different\n---\n\ndifferent\n"); err != nil {
		t.Fatalf("precreate conflicting skill: %v", err)
	}
	if _, _, err := candidateStore.Promote(candidate.Hash, activeStore, skill.ScopeProject); err == nil {
		t.Fatal("Promote succeeded despite conflicting active skill")
	}
	loaded, err := candidateStore.load(candidate.Hash)
	if err != nil {
		t.Fatalf("load after failed promote: %v", err)
	}
	if loaded.Status != CandidatePending {
		t.Fatalf("candidate status = %s, want pending after failed promote", loaded.Status)
	}
}

func TestRollbackPromotedCandidateRemovesOnlyMatchingSkill(t *testing.T) {
	candidateStore := NewCandidateStore(t.TempDir())
	candidate, err := candidateStore.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := candidateStore.RecordEvaluation(candidate.Hash, ScoreResult{Score: 0.9, Reason: "ok"}, GuardrailResult{Pass: true, Reason: "passed"}); err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: t.TempDir(), DisableBuiltins: true})
	updated, path, err := candidateStore.Promote(candidate.Hash, activeStore, skill.ScopeProject)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if updated.Status != CandidatePromoted {
		t.Fatalf("status = %s, want promoted", updated.Status)
	}
	rolledBack, err := candidateStore.Rollback(candidate.Hash, activeStore, "not useful after review")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolledBack.Status != CandidateRolledBack || !strings.Contains(rolledBack.ValidationReason, "not useful") {
		t.Fatalf("rolled back candidate = %+v", rolledBack)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("promoted skill still exists err=%v", err)
	}
}

func TestRollbackRefusesModifiedPromotedSkill(t *testing.T) {
	candidateStore := NewCandidateStore(t.TempDir())
	candidate, err := candidateStore.Create(validSkill("parser-helper"), BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := candidateStore.RecordEvaluation(candidate.Hash, ScoreResult{Score: 0.9, Reason: "ok"}, GuardrailResult{Pass: true, Reason: "passed"}); err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	activeStore := skill.New(skill.Options{HomeDir: t.TempDir(), ProjectRoot: t.TempDir(), DisableBuiltins: true})
	_, path, err := candidateStore.Promote(candidate.Hash, activeStore, skill.ScopeProject)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nname: parser-helper\ndescription: changed\n---\n\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := candidateStore.Rollback(candidate.Hash, activeStore, "rollback"); err == nil {
		t.Fatal("Rollback removed modified skill, want refusal")
	}
}

func validSkill(name string) skill.Skill {
	return skill.Skill{
		Name:        name,
		Description: "Helps fix parser bugs",
		Body:        "Use the parser checklist before editing.",
		RunAs:       skill.RunInline,
	}
}
