package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/config"
	"maddog/internal/provider"
	"maddog/internal/skill"
	"maddog/internal/skilleval"
)

const skillevalTestProviderKind = "skilleval-test-provider"

func init() {
	provider.Register(skillevalTestProviderKind, func(cfg provider.Config) (provider.Provider, error) {
		return skillevalTestProvider{cfg: cfg}, nil
	})
}

func TestSkillEvalCommandScoresBundleCandidateDryRun(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.json")
	candidatePath := filepath.Join(dir, "candidate.json")
	writeJSONFixture(t, bundlePath, skilleval.BundleV2{
		ID:      "bundle-a",
		Outcome: verifiedSkillEvalOutcomeWithErrors("done", 1),
	})
	writeJSONFixture(t, candidatePath, skilleval.Candidate{
		Hash:       "abc",
		Status:     skilleval.CandidatePending,
		Skill:      validScoredSkillForCLI("parser-helper"),
		Validation: skilleval.ValidationInfo{Valid: true},
	})

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"--bundle", bundlePath, "--candidate", candidatePath, "--dry-run", "--json", "--min-bundles", "1"})
		if rc == 0 {
			t.Fatal("skillevalCommand rc = 0, want dry-run guardrail failure")
		}
	})
	if !strings.Contains(out, "\"bundle_id\": \"bundle-a\"") || !strings.Contains(out, "\"guardrail_pass\": false") || !strings.Contains(out, "dry-run preview") {
		t.Fatalf("unexpected skilleval output: %s", out)
	}
	if !strings.Contains(out, "Use the parser checklist") {
		t.Fatalf("dry-run did not replay candidate body: %s", out)
	}
}

func TestSkillEvalCommandPersistsCandidateEvaluation(t *testing.T) {
	storeDir := t.TempDir()
	bundle, _, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
		SessionID: "session-a",
		Task:      "parse logs",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "parse logs"},
			{Role: provider.RoleAssistant, Content: "done"},
		},
		Outcome: verifiedSkillEvalOutcome("done"),
		Dir:     storeDir,
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	store := skilleval.NewCandidateStore(storeDir)
	candidate, err := store.Create(validScoredSkillForCLI("parser-helper"), *bundle, "parse logs")
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	_, bundlePath, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
		SessionID: "session-b",
		Task:      "parse logs held-out",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "parse held-out logs"},
			{Role: provider.RoleAssistant, Content: "done"},
		},
		Outcome: verifiedSkillEvalOutcome("done"),
		Dir:     storeDir,
	})
	if err != nil {
		t.Fatalf("CaptureBundle held-out: %v", err)
	}
	candidatePath := filepath.Join(storeDir, "candidates", candidate.Hash+".json")

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"--bundle", bundlePath, "--candidate", candidatePath, "--dry-run", "--json", "--min-bundles", "1", "--store-dir", storeDir})
		if rc == 0 {
			t.Fatal("skillevalCommand rc = 0, want dry-run guardrail failure")
		}
	})
	if !strings.Contains(out, "\"persisted\": true") {
		t.Fatalf("persisted marker missing: %s", out)
	}
	updated, err := store.Get(candidate.Hash)
	if err != nil {
		t.Fatalf("Get candidate: %v", err)
	}
	if updated.EvalScore == nil {
		t.Fatalf("EvalScore = nil, want persisted preview score")
	}
	if updated.GuardrailPass || !strings.Contains(updated.GuardrailReason, "dry-run preview") {
		t.Fatalf("guardrail = %v/%q, want persisted dry-run preview failure", updated.GuardrailPass, updated.GuardrailReason)
	}
}

func TestSkillEvalCommandRejectsSourceOrDuplicateBundles(t *testing.T) {
	storeDir := t.TempDir()
	source, sourcePath, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
		SessionID: "session-source",
		Task:      "parse logs",
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "parse logs"}},
		Outcome:   verifiedSkillEvalOutcome("done"),
		Dir:       storeDir,
	})
	if err != nil {
		t.Fatalf("CaptureBundle source: %v", err)
	}
	candidate, err := skilleval.NewCandidateStore(storeDir).Create(validScoredSkillForCLI("parser-helper"), *source, "parse logs")
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	candidatePath := filepath.Join(storeDir, "candidates", candidate.Hash+".json")

	errOut := captureStderr(t, func() {
		rc := skillevalCommand([]string{"--bundle", sourcePath, "--candidate", candidatePath, "--dry-run", "--json", "--min-bundles", "1"})
		if rc == 0 {
			t.Fatal("skillevalCommand rc = 0, want source bundle rejection")
		}
	})
	if !strings.Contains(errOut, "not held-out") {
		t.Fatalf("source bundle rejection missing: %s", errOut)
	}

	heldOutPath := filepath.Join(storeDir, "held-out.json")
	writeJSONFixture(t, heldOutPath, skilleval.BundleV2{
		ID:      "held-out",
		Outcome: verifiedSkillEvalOutcome("done"),
	})
	errOut = captureStderr(t, func() {
		rc := skillevalCommand([]string{"--bundle", heldOutPath, "--bundle", heldOutPath, "--candidate", candidatePath, "--dry-run", "--json", "--min-bundles", "1"})
		if rc == 0 {
			t.Fatal("skillevalCommand rc = 0, want duplicate bundle rejection")
		}
	})
	if !strings.Contains(errOut, "duplicate held-out bundle") {
		t.Fatalf("duplicate bundle rejection missing: %s", errOut)
	}
}

func TestSkillEvalCommandRequiresHeldOutBundlesByDefault(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.json")
	candidatePath := filepath.Join(dir, "candidate.json")
	writeJSONFixture(t, bundlePath, skilleval.BundleV2{
		ID:      "bundle-a",
		Outcome: verifiedSkillEvalOutcome("done"),
	})
	writeJSONFixture(t, candidatePath, skilleval.Candidate{
		Hash:       "abc",
		Status:     skilleval.CandidatePending,
		Skill:      validScoredSkillForCLI("parser-helper"),
		Validation: skilleval.ValidationInfo{Valid: true},
	})

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"--bundle", bundlePath, "--candidate", candidatePath, "--dry-run", "--json"})
		if rc == 0 {
			t.Fatal("skillevalCommand rc = 0, want guardrail failure for one bundle")
		}
	})
	if !strings.Contains(out, "\"guardrail_pass\": false") || !strings.Contains(out, "need at least 5 bundles") {
		t.Fatalf("unexpected one-bundle guardrail output: %s", out)
	}
}

func TestSkillEvalCommandEvaluatesRepeatedHeldOutBundles(t *testing.T) {
	dir := t.TempDir()
	candidatePath := filepath.Join(dir, "candidate.json")
	writeJSONFixture(t, candidatePath, skilleval.Candidate{
		Hash:       "abc",
		Status:     skilleval.CandidatePending,
		Skill:      validScoredSkillForCLI("parser-helper"),
		Validation: skilleval.ValidationInfo{Valid: true},
	})
	args := []string{"--candidate", candidatePath, "--dry-run", "--json"}
	for i := 0; i < 5; i++ {
		bundlePath := filepath.Join(dir, "bundle-"+string(rune('a'+i))+".json")
		writeJSONFixture(t, bundlePath, skilleval.BundleV2{
			ID:      "bundle-" + string(rune('a'+i)),
			Outcome: verifiedSkillEvalOutcome("done"),
		})
		args = append(args, "--bundle", bundlePath)
	}

	out := captureStdout(t, func() {
		rc := skillevalCommand(args)
		if rc == 0 {
			t.Fatal("skillevalCommand rc = 0, want dry-run guardrail failure")
		}
	})
	if !strings.Contains(out, "\"bundles\": 5") || !strings.Contains(out, "\"bundle_ids\"") || !strings.Contains(out, "\"guardrail_pass\": false") || !strings.Contains(out, "dry-run preview") {
		t.Fatalf("unexpected multi-bundle output: %s", out)
	}
	if !strings.Contains(out, "\"scores\"") || !strings.Contains(out, "\"replays\"") {
		t.Fatalf("multi-bundle details missing: %s", out)
	}
}

func TestSkillEvalCommandRunsConfiguredProviderReplay(t *testing.T) {
	isolateCLIConfigHome(t)
	t.Setenv("MADDOG_TEST_KEY", "test-key")
	userConfig := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`
default_model = "local"

[codegraph]
enabled = false

[[providers]]
name = "local"
kind = "skilleval-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "MADDOG_TEST_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.json")
	candidatePath := filepath.Join(dir, "candidate.json")
	writeJSONFixture(t, bundlePath, skilleval.BundleV2{
		ID:      "bundle-a",
		Outcome: verifiedSkillEvalOutcomeWithErrors("done", 1),
	})
	writeJSONFixture(t, candidatePath, skilleval.Candidate{
		Hash:       "abc",
		Status:     skilleval.CandidatePending,
		Skill:      validScoredSkillForCLI("parser-helper"),
		Validation: skilleval.ValidationInfo{Valid: true},
	})

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"--bundle", bundlePath, "--candidate", candidatePath, "--json", "--min-bundles", "1"})
		if rc != 0 {
			t.Fatalf("skillevalCommand rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "provider replay answer") {
		t.Fatalf("provider replay output missing: %s", out)
	}
	if !strings.Contains(out, "\"score\": 0.92") || !strings.Contains(out, "provider scorer") {
		t.Fatalf("provider scorer output missing: %s", out)
	}
	for _, want := range []string{
		"\"mode\": \"provider_replay\"",
		"\"provider\": \"local\"",
		"\"model_ref\": \"local\"",
		"\"promotion_grade\": true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("provider provenance %s missing: %s", want, out)
		}
	}
}

func TestSkillEvalListCommandPrintsCandidateStates(t *testing.T) {
	dir := t.TempDir()
	candidates := filepath.Join(dir, "candidates")
	if err := os.MkdirAll(candidates, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFixture(t, filepath.Join(candidates, "a.json"), skilleval.Candidate{Hash: "a", Status: skilleval.CandidatePending, Skill: validScoredSkillForCLI("a")})
	writeJSONFixture(t, filepath.Join(candidates, "b.json"), skilleval.Candidate{Hash: "b", Status: skilleval.CandidateRejected, Skill: validScoredSkillForCLI("b")})

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"list", "--dir", dir, "--json"})
		if rc != 0 {
			t.Fatalf("skilleval list rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "\"status\": \"pending\"") || !strings.Contains(out, "\"status\": \"rejected\"") {
		t.Fatalf("list output missing states: %s", out)
	}
}

func writeJSONFixture(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func validScoredSkillForCLI(name string) skill.Skill {
	return skill.Skill{
		Name:        name,
		Description: "Helps fix parser bugs",
		Body:        "Use the parser checklist before editing.",
		RunAs:       skill.RunInline,
	}
}

func TestSkillEvalReviewCommandApprovesBundle(t *testing.T) {
	bundle, bundlePath, err := skilleval.CaptureBundle(skilleval.CaptureOptions{
		SessionID: "reviewed",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "fix parser"},
			{Role: provider.RoleAssistant, Content: "parser fixed"},
		},
		Outcome: skilleval.OutcomeInfo{
			Confidence:       skilleval.OutcomeConfidenceUnverified,
			ConfidenceReason: "no verified signal",
		},
		Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if bundle.Outcome.Confidence == skilleval.OutcomeConfidenceVerified {
		t.Fatal("test setup expected unverified bundle")
	}

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"review", "--bundle", bundlePath, "--approve", "--reviewer", "qa", "--reason", "manual acceptance passed", "--json"})
		if rc != 0 {
			t.Fatalf("skilleval review rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "\"approved\": true") || !strings.Contains(out, "\"confidence\": \"verified\"") {
		t.Fatalf("unexpected review output: %s", out)
	}
	updated, err := skilleval.LoadBundle(bundlePath)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if !updated.Review.Approved || updated.Review.Reviewer != "qa" || updated.Outcome.Confidence != skilleval.OutcomeConfidenceVerified {
		t.Fatalf("reviewed bundle = %+v, want approved verified bundle", updated)
	}
	if !updated.Outcome.Success || !updated.Outcome.GoalMet || updated.Outcome.HumanReviews != 1 {
		t.Fatalf("reviewed outcome = %+v, want human verified success", updated.Outcome)
	}
}

func verifiedSkillEvalOutcome(finalAnswer string) skilleval.OutcomeInfo {
	return verifiedSkillEvalOutcomeWithErrors(finalAnswer, 0)
}

func verifiedSkillEvalOutcomeWithErrors(finalAnswer string, toolErrors int) skilleval.OutcomeInfo {
	return skilleval.OutcomeInfo{
		Success:          true,
		GoalMet:          true,
		Confidence:       skilleval.OutcomeConfidenceVerified,
		ConfidenceReason: "test fixture verified outcome",
		FinalAnswer:      finalAnswer,
		ToolErrors:       toolErrors,
	}
}

type skillevalTestProvider struct {
	cfg provider.Config
}

func (p skillevalTestProvider) Name() string { return p.cfg.Name }

func (p skillevalTestProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	text := "provider replay answer"
	if len(req.Messages) > 0 && strings.Contains(strings.ToLower(req.Messages[0].Content), "score the replayed") {
		text = "0.92 provider scorer"
	}
	_ = p.cfg
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
