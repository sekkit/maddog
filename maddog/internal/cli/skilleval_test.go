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
		Outcome: skilleval.OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "done", ToolErrors: 1},
	})
	writeJSONFixture(t, candidatePath, skilleval.Candidate{
		Hash:       "abc",
		Status:     skilleval.CandidatePending,
		Skill:      validScoredSkillForCLI("parser-helper"),
		Validation: skilleval.ValidationInfo{Valid: true},
	})

	out := captureStdout(t, func() {
		rc := skillevalCommand([]string{"--bundle", bundlePath, "--candidate", candidatePath, "--dry-run", "--json", "--min-bundles", "1"})
		if rc != 0 {
			t.Fatalf("skillevalCommand rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "\"bundle_id\": \"bundle-a\"") || !strings.Contains(out, "\"guardrail_pass\": true") {
		t.Fatalf("unexpected skilleval output: %s", out)
	}
	if !strings.Contains(out, "Use the parser checklist") {
		t.Fatalf("dry-run did not replay candidate body: %s", out)
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
		Outcome: skilleval.OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "done", ToolErrors: 1},
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

type skillevalTestProvider struct {
	cfg provider.Config
}

func (p skillevalTestProvider) Name() string { return p.cfg.Name }

func (p skillevalTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "provider replay answer"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
