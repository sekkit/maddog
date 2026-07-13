package skilleval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/provider"
	"maddog/internal/tool"
)

func TestEvaluateCandidateProviderReplayIsDiagnosticOnly(t *testing.T) {
	dir := t.TempDir()
	sourcePath := writeEvaluationBundle(t, dir, "source", "source answer")
	candidate := Candidate{
		Hash:             "abc",
		Status:           CandidatePending,
		Skill:            validSkill("parser-helper"),
		SourceBundleID:   "source",
		SourceBundlePath: sourcePath,
		SourceTask:       "fix parser",
		Validation:       ValidationInfo{Valid: true},
	}
	paths := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		paths = append(paths, writeEvaluationBundle(t, dir, "held-out-"+string(rune('a'+i)), "provider replay answer"))
	}
	prov := &evaluationScriptProvider{}

	result, err := EvaluateCandidate(context.Background(), EvaluationRequest{
		Candidate:   candidate,
		BundlePaths: paths,
		Provider:    prov,
		Scorer:      prov,
		MinBundles:  5,
		MinScore:    0.7,
	})
	if err != nil {
		t.Fatalf("EvaluateCandidate: %v", err)
	}
	if result.Guardrail.Pass || result.PromotionGrade || result.Mode != EvaluationModeProviderReplay || !strings.Contains(result.Guardrail.Reason, "paired") {
		t.Fatalf("evaluation result = %+v, want diagnostic-only provider replay", result)
	}
	if result.Provider != "evaluation-script" || len(result.BundleIDs) != 5 || result.Score.Score < 0.9 {
		t.Fatalf("evaluation summary = %+v", result)
	}
	if prov.replayCalls != 5 || prov.scoreCalls != 5 {
		t.Fatalf("provider calls replay=%d score=%d, want 5/5", prov.replayCalls, prov.scoreCalls)
	}
}

func TestEvaluateCandidateUsesAgentReplayWhenToolsProvided(t *testing.T) {
	dir := t.TempDir()
	bundlePath := writeEvaluationBundle(t, dir, "held-out-a", "final answer after tool")
	reg := tool.NewRegistry()
	reg.Add(staticReplayTool{name: "inspect_fixture", result: "fixture says ok"})
	replayProvider := &scriptReplayProvider{turns: []providerTurn{
		{call: &provider.ToolCall{ID: "call-1", Name: "inspect_fixture", Arguments: `{}`}},
		{text: "final answer after tool"},
	}}
	scorer := &evaluationScriptProvider{}

	result, err := EvaluateCandidate(context.Background(), EvaluationRequest{
		Candidate: Candidate{
			Hash:       "abc",
			Status:     CandidatePending,
			Skill:      validSkill("parser-helper"),
			Validation: ValidationInfo{Valid: true},
		},
		BundlePaths: []string{bundlePath},
		Provider:    replayProvider,
		Scorer:      scorer,
		Tools:       reg,
		MinBundles:  1,
		MinScore:    0.7,
	})
	if err != nil {
		t.Fatalf("EvaluateCandidate: %v", err)
	}
	if result.Mode != EvaluationModeAgentReplay {
		t.Fatalf("Mode = %q, want %q", result.Mode, EvaluationModeAgentReplay)
	}
	if len(result.Replays) != 1 || result.Replays[0].TotalTurns < 2 || result.Replays[0].FinalAnswer != "final answer after tool" {
		t.Fatalf("replays = %+v, want agent tool-loop replay", result.Replays)
	}
}

func TestEvaluateCandidateRejectsSourceBundleAsHeldOut(t *testing.T) {
	dir := t.TempDir()
	sourcePath := writeEvaluationBundle(t, dir, "source", "source answer")
	candidate := Candidate{
		Hash:             "abc",
		Status:           CandidatePending,
		Skill:            validSkill("parser-helper"),
		SourceBundleID:   "source",
		SourceBundlePath: sourcePath,
		SourceTask:       "fix parser",
		Validation:       ValidationInfo{Valid: true},
	}
	_, err := EvaluateCandidate(context.Background(), EvaluationRequest{
		Candidate:   candidate,
		BundlePaths: []string{sourcePath},
		DryRun:      true,
		MinBundles:  1,
	})
	if err == nil || !strings.Contains(err.Error(), "not held-out") {
		t.Fatalf("EvaluateCandidate source bundle err = %v, want held-out rejection", err)
	}
}

func TestEvaluateCandidateDryRunIsPreviewOnly(t *testing.T) {
	dir := t.TempDir()
	candidate := Candidate{
		Hash:       "abc",
		Status:     CandidatePending,
		Skill:      validSkill("parser-helper"),
		SourceTask: "fix parser",
		Validation: ValidationInfo{Valid: true},
	}
	paths := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		paths = append(paths, writeEvaluationBundle(t, dir, "held-out-"+string(rune('a'+i)), "done"))
	}
	result, err := EvaluateCandidate(context.Background(), EvaluationRequest{
		Candidate:   candidate,
		BundlePaths: paths,
		DryRun:      true,
		MinBundles:  5,
		MinScore:    0.7,
	})
	if err != nil {
		t.Fatalf("EvaluateCandidate dry run: %v", err)
	}
	if result.PromotionGrade || result.Guardrail.Pass || !strings.Contains(result.Guardrail.Reason, "dry-run preview") {
		t.Fatalf("dry-run result = %+v, want preview-only guardrail failure", result)
	}
}

func TestEvaluateCandidateUnparseablePromotionScorerCannotPass(t *testing.T) {
	dir := t.TempDir()
	candidate := Candidate{
		Hash:       "abc",
		Status:     CandidatePending,
		Skill:      validSkill("parser-helper"),
		SourceTask: "fix parser",
		Validation: ValidationInfo{Valid: true},
	}
	paths := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		paths = append(paths, writeEvaluationBundle(t, dir, "held-out-"+string(rune('a'+i)), "provider replay answer"))
	}
	prov := &evaluationUnparseableScorerProvider{}

	result, err := EvaluateCandidate(context.Background(), EvaluationRequest{
		Candidate:   candidate,
		BundlePaths: paths,
		Provider:    prov,
		Scorer:      prov,
		MinBundles:  5,
		MinScore:    0.7,
	})
	if err != nil {
		t.Fatalf("EvaluateCandidate: %v", err)
	}
	if result.PromotionGrade || result.Guardrail.Pass {
		t.Fatalf("evaluation result = %+v, want unparseable scorer to block promotion-grade pass", result)
	}
	if !strings.Contains(result.Guardrail.Reason, "below threshold") {
		t.Fatalf("guardrail reason = %q, want score threshold failure", result.Guardrail.Reason)
	}
}

func writeEvaluationBundle(t *testing.T, dir, id, finalAnswer string) string {
	t.Helper()
	bundle := BundleV2{
		Version: 2,
		ID:      id,
		Outcome: OutcomeInfo{
			Success:          true,
			GoalMet:          true,
			Confidence:       OutcomeConfidenceVerified,
			ConfidenceReason: "test fixture verified outcome",
			FinalAnswer:      finalAnswer,
		},
		CreatedAt: time.Date(2026, 7, 3, 1, 2, 3, 0, time.UTC),
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type evaluationScriptProvider struct {
	replayCalls int
	scoreCalls  int
}

func (p *evaluationScriptProvider) Name() string { return "evaluation-script" }

func (p *evaluationScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	text := "provider replay answer"
	if len(req.Messages) > 0 && strings.Contains(strings.ToLower(req.Messages[0].Content), "score the replayed") {
		p.scoreCalls++
		text = "0.92 provider scorer"
	} else {
		p.replayCalls++
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type evaluationUnparseableScorerProvider struct {
	replayCalls int
	scoreCalls  int
}

func (p *evaluationUnparseableScorerProvider) Name() string { return "evaluation-unparseable" }

func (p *evaluationUnparseableScorerProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	text := "provider replay answer"
	if len(req.Messages) > 0 && strings.Contains(strings.ToLower(req.Messages[0].Content), "score the replayed") {
		p.scoreCalls++
		text = "looks good"
	} else {
		p.replayCalls++
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
