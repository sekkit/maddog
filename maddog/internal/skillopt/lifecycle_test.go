package skillopt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/skill"
)

func TestPromoteBestAndRollbackRestoreExactSnapshot(t *testing.T) {
	root := t.TempDir()
	original := "---\nname: deploy-me\ndescription: deploy fixture\nallowed-tools: read_file\n---\n\nold body\n\n<!-- preserve exact formatting -->\n"
	path := writeProjectSkill(t, root, "deploy-me", original)
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	baseline, ok := skills.Read("deploy-me")
	if !ok {
		t.Fatal("baseline skill not discovered")
	}
	best := baseline
	best.Body = "new improved body"
	runs := NewJSONRunStore(filepath.Join(root, ".maddog", "skillopt", "runs"))
	createCompletedRun(t, runs, "deploy", baseline, best)

	run, err := PromoteBest(context.Background(), runs, "deploy", skills, skill.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if run.Promotion == nil || run.Promotion.PromotedAt.IsZero() || run.Promotion.Previous == nil || !run.Promotion.Previous.Existed {
		t.Fatalf("promotion metadata = %+v", run.Promotion)
	}
	promoted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(promoted), "new improved body") || skill.ContentHash(string(promoted)) != run.Promotion.PromotedHash {
		t.Fatalf("promoted bytes = %q", promoted)
	}

	run, err = RollbackPromotion(context.Background(), runs, "deploy", skills, "quality regression")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != original {
		t.Fatalf("rollback did not restore exact bytes\ngot:  %q\nwant: %q", restored, original)
	}
	if !run.Promotion.RolledBack || run.Promotion.RollbackReason != "quality regression" {
		t.Fatalf("rollback metadata = %+v", run.Promotion)
	}
}

func TestPromotionCASRejectsConcurrentEdits(t *testing.T) {
	root := t.TempDir()
	path := writeProjectSkill(t, root, "deploy-me", "---\nname: deploy-me\ndescription: fixture\n---\n\nold body\n")
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	baseline, _ := skills.Read("deploy-me")
	best := baseline
	best.Body = "new body"
	runs := NewJSONRunStore(filepath.Join(root, "runs"))
	createCompletedRun(t, runs, "cas", baseline, best)

	if err := os.WriteFile(path, []byte("---\nname: deploy-me\ndescription: fixture\n---\n\nuser edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteBest(context.Background(), runs, "cas", skills, skill.ScopeProject); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("promotion concurrent-edit error = %v", err)
	}
}

func TestPromotionCASRejectsFormattingOnlyConcurrentEdit(t *testing.T) {
	root := t.TempDir()
	original := "---\nname: deploy-me\ndescription: fixture\n---\n\nold body\n"
	path := writeProjectSkill(t, root, "deploy-me", original)
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	baseline, _ := skills.Read("deploy-me")
	best := baseline
	best.Body = "new body"
	runs := NewJSONRunStore(filepath.Join(root, "runs"))
	createCompletedRun(t, runs, "format-cas", baseline, best)

	if err := os.WriteFile(path, []byte(original+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteBest(context.Background(), runs, "format-cas", skills, skill.ScopeProject); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("promotion formatting-only edit error = %v", err)
	}
}

func TestRollbackCASRejectsPostPromotionEdit(t *testing.T) {
	root := t.TempDir()
	path := writeProjectSkill(t, root, "deploy-me", "---\nname: deploy-me\ndescription: fixture\n---\n\nold body\n")
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	baseline, _ := skills.Read("deploy-me")
	best := baseline
	best.Body = "new body"
	runs := NewJSONRunStore(filepath.Join(root, "runs"))
	createCompletedRun(t, runs, "rollback-cas", baseline, best)
	if _, err := PromoteBest(context.Background(), runs, "rollback-cas", skills, skill.ScopeProject); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: deploy-me\ndescription: fixture\n---\n\npost promotion edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RollbackPromotion(context.Background(), runs, "rollback-cas", skills, "test"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("rollback concurrent-edit error = %v", err)
	}
}

func createCompletedRun(t *testing.T, store RunStore, id string, baseline, best skill.Skill) {
	t.Helper()
	baseArtifact, err := newArtifact(baseline)
	if err != nil {
		t.Fatal(err)
	}
	bestArtifact, err := newArtifact(best)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 2, 0, 0, 0, time.UTC)
	run := &Run{
		SchemaVersion: SchemaVersion, ID: id, Status: StatusCompleted,
		BaselineContentHash: sourceContentHash(baseline),
		BaselineRevisionID:  "base", CurrentRevisionID: "best", BestRevisionID: "best",
		Revisions: []Revision{
			{ID: "base", Artifact: baseArtifact, CreatedAt: now},
			{ID: "best", ParentID: "base", Round: 1, Artifact: bestArtifact, CreatedAt: now},
		},
		Test: TestRecord{RevisionID: "best", Completed: true}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func writeProjectSkill(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, ".maddog", "skills", name, skill.SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
