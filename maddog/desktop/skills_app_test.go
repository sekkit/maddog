package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/config"
	"maddog/internal/control"
	"maddog/internal/plugin"
	"maddog/internal/provider"
	"maddog/internal/skill"
	"maddog/internal/skilleval"
)

const desktopSkillEvalProviderKind = "desktop-skilleval-test-provider"

func init() {
	provider.Register(desktopSkillEvalProviderKind, func(cfg provider.Config) (provider.Provider, error) {
		return desktopSkillEvalProvider{name: cfg.Name}, nil
	})
}

func TestCapabilitiesProjectsSkillCandidates(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	store := skilleval.NewCandidateStore(filepath.Join(dir, config.ProjectConventionDir, "skilleval"))
	candidate, err := store.Create(desktopTestSkill("parser-helper"), skilleval.BundleV2{ID: "bundle-a", Path: "bundle-a.json"}, "fix parser")
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	if _, err := store.RecordEvaluation(candidate.Hash, skilleval.ScoreResult{Score: 0.91, Reason: "passed"}, skilleval.GuardrailResult{Pass: true, Reason: "guardrail passed"}); err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}

	view := app.Capabilities()
	if len(view.SkillCandidates) != 1 {
		t.Fatalf("SkillCandidates = %+v, want one candidate", view.SkillCandidates)
	}
	got := view.SkillCandidates[0]
	if got.Hash != candidate.Hash || got.Name != "parser-helper" || got.Status != string(skilleval.CandidatePending) {
		t.Fatalf("candidate view = %+v", got)
	}
	if got.SourceBundleID != "bundle-a" || got.Score == nil || *got.Score < 0.9 {
		t.Fatalf("candidate evidence = %+v", got)
	}
	if got.GuardrailPass == nil || !*got.GuardrailPass || got.SourceTask != "fix parser" || !strings.Contains(got.TargetRoot, filepath.Join(config.ProjectConventionDir, skill.SkillsDirname)) {
		t.Fatalf("candidate detail = %+v", got)
	}

	settings := app.SkillsSettings()
	if len(settings.SkillCandidates) != 1 || settings.SkillCandidates[0].Hash != candidate.Hash {
		t.Fatalf("SkillsSettings SkillCandidates = %+v, want candidate %s", settings.SkillCandidates, candidate.Hash)
	}
}

func TestEvaluateSkillCandidateFromDesktopRecordsDryRunPreview(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	evalDir := filepath.Join(dir, config.ProjectConventionDir, "skilleval")
	store := skilleval.NewCandidateStore(evalDir)
	testSkill := desktopTestSkill("parser-helper")
	bundle, _, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
		SessionID: "session-replay",
		Task:      "fix parser",
		Skills:    []skill.Skill{testSkill},
		Outcome: skilleval.OutcomeInfo{
			Success:          true,
			GoalMet:          true,
			Confidence:       skilleval.OutcomeConfidenceVerified,
			ConfidenceReason: "test fixture verified outcome",
			FinalAnswer:      testSkill.Body,
			TotalTurns:       1,
		},
		Dir: evalDir,
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	candidate, err := store.Create(testSkill, *bundle, "fix parser")
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}

	got, err := app.EvaluateSkillCandidate(candidate.Hash, SkillCandidateEvaluationRequest{DryRun: true})
	if err != nil {
		t.Fatalf("EvaluateSkillCandidate: %v", err)
	}
	if got.Hash != candidate.Hash || got.Score == nil || *got.Score < 0.7 {
		t.Fatalf("evaluated candidate score = %+v", got)
	}
	if got.PromotionGrade || got.EvaluationMode != skilleval.EvaluationModeDryRunPreview {
		t.Fatalf("evaluated candidate provenance = %+v, want preview-only", got)
	}
	if got.GuardrailPass == nil || *got.GuardrailPass || !strings.Contains(got.GuardrailReason, "not verified") {
		t.Fatalf("evaluated candidate guardrail = %+v, want dry-run verification rejection", got)
	}

	if _, err := app.PromoteSkillCandidate(candidate.Hash); err == nil || !strings.Contains(err.Error(), "promotion-grade") {
		t.Fatalf("PromoteSkillCandidate after preview err = %v, want promotion-grade failure", err)
	}
}

func TestEvaluateSkillCandidateFromDesktopRunsProviderReplayPromotionGrade(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("MADDOG_DESKTOP_SKILLEVAL_KEY", "test-key")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "local"

[codegraph]
enabled = false

[[providers]]
name = "local"
kind = "desktop-skilleval-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "MADDOG_DESKTOP_SKILLEVAL_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := robustTempDir(t)
	t.Chdir(dir)
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	evalDir := filepath.Join(dir, config.ProjectConventionDir, "skilleval")
	store := skilleval.NewCandidateStore(evalDir)
	testSkill := desktopTestSkill("parser-helper")
	source, _, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
		SessionID: "session-source",
		Task:      "fix parser",
		Skills:    []skill.Skill{testSkill},
		Outcome:   desktopVerifiedOutcome(testSkill.Body),
		Dir:       evalDir,
	})
	if err != nil {
		t.Fatalf("CaptureBundle source: %v", err)
	}
	candidate, err := store.Create(testSkill, *source, "fix parser")
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	heldOut := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		_, path, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
			SessionID: "session-held-out-" + string(rune('a'+i)),
			Task:      "fix parser held-out",
			Skills:    []skill.Skill{testSkill},
			Outcome:   desktopVerifiedOutcome("provider replay answer"),
			Dir:       evalDir,
		})
		if err != nil {
			t.Fatalf("CaptureBundle held-out: %v", err)
		}
		heldOut = append(heldOut, path)
	}

	got, err := app.EvaluateSkillCandidate(candidate.Hash, SkillCandidateEvaluationRequest{BundlePaths: heldOut})
	if err != nil {
		t.Fatalf("EvaluateSkillCandidate provider replay: %v", err)
	}
	if got.GuardrailPass == nil || !*got.GuardrailPass || !got.PromotionGrade || got.EvaluationMode != skilleval.EvaluationModeProviderReplay || got.EvaluationProvider != "local" || got.EvaluationModelRef != "local" {
		t.Fatalf("provider evaluation = %+v, want promotion-grade pass", got)
	}
	if got.EvaluationBundleCount != 5 || got.Score == nil || *got.Score < 0.9 {
		t.Fatalf("provider evaluation summary = %+v", got)
	}
	if _, err := app.PromoteSkillCandidate(candidate.Hash); err != nil {
		t.Fatalf("PromoteSkillCandidate after promotion-grade evaluation: %v", err)
	}
}

func TestPromoteAndRejectSkillCandidateFromDesktop(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	store := skilleval.NewCandidateStore(filepath.Join(dir, config.ProjectConventionDir, "skilleval"))
	promotable, err := store.Create(desktopTestSkill("parser-helper"), skilleval.BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create promotable: %v", err)
	}
	recordDesktopPromotionGradeEvaluation(t, store, promotable.Hash)

	path, err := app.PromoteSkillCandidate(promotable.Hash)
	if err != nil {
		t.Fatalf("PromoteSkillCandidate: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join(dir, config.ProjectConventionDir, skill.SkillsDirname)) {
		t.Fatalf("promoted path = %q, want project skill root", path)
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "Use the parser checklist") {
		t.Fatalf("promoted skill file raw=%q err=%v", raw, err)
	}

	rejectable, err := store.Create(desktopTestSkill("docs-helper"), skilleval.BundleV2{ID: "bundle-b"}, "write docs")
	if err != nil {
		t.Fatalf("Create rejectable: %v", err)
	}
	if err := app.RejectSkillCandidate(rejectable.Hash, "not useful"); err != nil {
		t.Fatalf("RejectSkillCandidate: %v", err)
	}
	view := app.Capabilities()
	for _, c := range view.SkillCandidates {
		if c.Hash == rejectable.Hash && c.Status == string(skilleval.CandidateRejected) && strings.Contains(c.ValidationReason, "not useful") {
			return
		}
	}
	t.Fatalf("rejected candidate missing from view: %+v", view.SkillCandidates)
}

func TestRollbackSkillCandidateFromDesktop(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	store := skilleval.NewCandidateStore(filepath.Join(dir, config.ProjectConventionDir, "skilleval"))
	promotable, err := store.Create(desktopTestSkill("parser-helper"), skilleval.BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create promotable: %v", err)
	}
	recordDesktopPromotionGradeEvaluation(t, store, promotable.Hash)
	path, err := app.PromoteSkillCandidate(promotable.Hash)
	if err != nil {
		t.Fatalf("PromoteSkillCandidate: %v", err)
	}
	if err := app.RollbackSkillCandidate(promotable.Hash, "not needed"); err != nil {
		t.Fatalf("RollbackSkillCandidate: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("promoted skill path still exists err=%v", err)
	}
	view := app.Capabilities()
	for _, c := range view.SkillCandidates {
		if c.Hash == promotable.Hash && c.Status == string(skilleval.CandidateRolledBack) && strings.Contains(c.ValidationReason, "not needed") {
			if len(c.Audit) < 2 || c.Audit[0].Action != "promote" || c.Audit[1].Action != "rollback" {
				t.Fatalf("rolled back candidate audit = %+v, want promote and rollback", c.Audit)
			}
			return
		}
	}
	t.Fatalf("rolled back candidate missing from view: %+v", view.SkillCandidates)
}

func TestCapabilitiesProjectsFailedGuardrailExplicitly(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = dir
	defer app.activeCtrl().Close()

	store := skilleval.NewCandidateStore(filepath.Join(dir, config.ProjectConventionDir, "skilleval"))
	candidate, err := store.Create(desktopTestSkill("parser-helper"), skilleval.BundleV2{ID: "bundle-a"}, "fix parser")
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	if _, err := store.RecordEvaluation(candidate.Hash, skilleval.ScoreResult{Score: 0.91, Reason: "passed"}, skilleval.GuardrailResult{Pass: false, Reason: "regression"}); err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	view := app.Capabilities()
	if len(view.SkillCandidates) != 1 || view.SkillCandidates[0].GuardrailPass == nil || *view.SkillCandidates[0].GuardrailPass {
		t.Fatalf("guardrail failed candidate = %+v", view.SkillCandidates)
	}
}

func desktopTestSkill(name string) skill.Skill {
	return skill.Skill{
		Name:        name,
		Description: "Helps with parser work",
		Body:        "Use the parser checklist before editing.",
		RunAs:       skill.RunInline,
	}
}

func desktopVerifiedOutcome(finalAnswer string) skilleval.OutcomeInfo {
	return skilleval.OutcomeInfo{
		Success:          true,
		GoalMet:          true,
		Confidence:       skilleval.OutcomeConfidenceVerified,
		ConfidenceReason: "test fixture verified outcome",
		FinalAnswer:      finalAnswer,
		TotalTurns:       1,
	}
}

func recordDesktopPromotionGradeEvaluation(t *testing.T, store *skilleval.CandidateStore, hash string) {
	t.Helper()
	if _, err := store.RecordEvaluationWithProvenance(hash, skilleval.ScoreResult{Score: 0.92, Reason: "passed"}, skilleval.GuardrailResult{Pass: true, Reason: "passed"}, skilleval.EvaluationProvenance{
		Mode:           skilleval.EvaluationModeProviderReplay,
		Provider:       "test-provider",
		BundleIDs:      []string{"held-out-a", "held-out-b", "held-out-c", "held-out-d", "held-out-e"},
		PromotionGrade: true,
	}); err != nil {
		t.Fatalf("RecordEvaluationWithProvenance: %v", err)
	}
}

type desktopSkillEvalProvider struct {
	name string
}

func (p desktopSkillEvalProvider) Name() string { return p.name }

func (p desktopSkillEvalProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	text := "provider replay answer"
	if len(req.Messages) > 0 && strings.Contains(strings.ToLower(req.Messages[0].Content), "score the replayed") {
		text = "0.92 provider scorer"
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
