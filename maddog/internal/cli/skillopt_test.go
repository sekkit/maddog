package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/config"
	"maddog/internal/skill"
	"maddog/internal/skillopt"
)

func TestSkillOptOptimizeRequiresProjectEnablement(t *testing.T) {
	isolateCLIConfigHome(t)
	root := t.TempDir()
	withWorkingDirectory(t, root)
	errOut := captureStderr(t, func() {
		rc := skilloptCommand([]string{"optimize", "--skill", "x", "--manifest", "missing.json", "--suite", "missing"})
		if rc == 0 {
			t.Fatal("disabled optimize returned success")
		}
	})
	if !strings.Contains(errOut, "disabled") {
		t.Fatalf("disabled error missing: %s", errOut)
	}
}

func TestSkillOptStatusAndCancelLifecycle(t *testing.T) {
	isolateCLIConfigHome(t)
	root := t.TempDir()
	withWorkingDirectory(t, root)
	store := skillopt.NewJSONRunStore(filepath.Join(root, ".maddog", "skillopt", "runs"))
	now := time.Now().UTC()
	decision := skillopt.Decision{Accepted: false, HardDelta: -0.25, SoftDelta: 0.125, Reason: "hard score regressed"}
	if err := store.Create(context.Background(), &skillopt.Run{
		SchemaVersion: skillopt.SchemaVersion,
		ID:            "status-run",
		Status:        skillopt.StatusPending,
		Rounds: []skillopt.RoundRecord{
			{Number: 1, Decision: &decision, Completed: true},
			{Number: 2, Stage: skillopt.StageTraining},
		},
		Rejected:  []skillopt.RejectedCandidate{{Round: 1, RevisionID: "candidate-1", Decision: decision}},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if rc := skilloptCommand([]string{"status", "--run", "status-run", "--json"}); rc != 0 {
			t.Fatalf("status rc = %d", rc)
		}
	})
	if !strings.Contains(out, `"id": "status-run"`) || !strings.Contains(out, `"status": "pending"`) ||
		!strings.Contains(out, `"rounds":`) || !strings.Contains(out, `"rejected":`) ||
		!strings.Contains(out, `"hard_delta": -0.25`) || !strings.Contains(out, `"revision_id": "candidate-1"`) {
		t.Fatalf("status output = %s", out)
	}
	humanOut := captureStdout(t, func() {
		if rc := skilloptCommand([]string{"status", "--run", "status-run"}); rc != 0 {
			t.Fatalf("status rc = %d", rc)
		}
	})
	if !strings.Contains(humanOut, "gate: round=1 accepted=false hard_delta=-0.25 soft_delta=0.125 reason=hard score regressed") {
		t.Fatalf("human status output = %s", humanOut)
	}
	if rc := skilloptCommand([]string{"cancel", "--run", "status-run"}); rc != 0 {
		t.Fatalf("cancel rc = %d", rc)
	}
	run, err := store.Load(context.Background(), "status-run")
	if err != nil || !run.CancelRequested {
		t.Fatalf("cancel checkpoint = %+v, %v", run, err)
	}
}

func TestSkillOptPromoteRequiresApprovalAndRollbackRestoresBytes(t *testing.T) {
	isolateCLIConfigHome(t)
	root := t.TempDir()
	withWorkingDirectory(t, root)
	if err := os.WriteFile(filepath.Join(root, config.ProjectConfigFilename), []byte("[skills.optimization]\nenabled = true\nrequire_approval = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := "---\nname: deploy-cli\ndescription: cli fixture\n---\n\nold body\n"
	skillPath := filepath.Join(root, ".maddog", "skills", "deploy-cli", skill.SkillFile)
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	skills := skill.New(skill.Options{ProjectRoot: root, ProjectOnly: true})
	baseline, ok := skills.Read("deploy-cli")
	if !ok {
		t.Fatal("skill not found")
	}
	best := baseline
	best.Body = "new body"
	store := skillopt.NewJSONRunStore(filepath.Join(root, ".maddog", "skillopt", "runs"))
	now := time.Now().UTC()
	run := &skillopt.Run{
		SchemaVersion: skillopt.SchemaVersion, ID: "deploy-cli-run", Status: skillopt.StatusCompleted,
		BaselineRevisionID: "base", CurrentRevisionID: "best", BestRevisionID: "best",
		Revisions: []skillopt.Revision{
			{ID: "base", Artifact: skillopt.Artifact{Skill: baseline, Digest: "base"}, CreatedAt: now},
			{ID: "best", ParentID: "base", Artifact: skillopt.Artifact{Skill: best, Digest: "best"}, CreatedAt: now},
		},
		Test: skillopt.TestRecord{RevisionID: "best", Completed: true}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if rc := skilloptCommand([]string{"promote", "--run", "deploy-cli-run"}); rc == 0 {
		t.Fatal("promotion without approval returned success")
	}
	if rc := skilloptCommand([]string{"promote", "--run", "deploy-cli-run", "--yes"}); rc != 0 {
		t.Fatalf("promotion rc = %d", rc)
	}
	promoted, _ := os.ReadFile(skillPath)
	if !strings.Contains(string(promoted), "new body") {
		t.Fatalf("skill not promoted: %s", promoted)
	}
	if rc := skilloptCommand([]string{"rollback", "--run", "deploy-cli-run", "--yes", "--reason", "fixture"}); rc != 0 {
		t.Fatalf("rollback rc = %d", rc)
	}
	restored, _ := os.ReadFile(skillPath)
	if string(restored) != original {
		t.Fatalf("rollback bytes = %q, want %q", restored, original)
	}
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
