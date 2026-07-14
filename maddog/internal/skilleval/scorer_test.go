package skilleval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestScoreReplayUsesRuleSignalsAndOptionalFrontier(t *testing.T) {
	original := OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "done", ToolErrors: 2}
	replayed := OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "done", ToolErrors: 0}
	got, err := ScoreReplay(context.Background(), nil, original, replayed)
	if err != nil {
		t.Fatalf("ScoreReplay rule: %v", err)
	}
	if got.Score < 0.9 || !strings.Contains(got.Reason, "rule") {
		t.Fatalf("rule score = %+v, want strong deterministic score", got)
	}

	frontier := &scriptReplayProvider{turns: []providerTurn{{text: "0.42 frontier says weak"}}}
	got, err = ScoreReplay(context.Background(), frontier, original, replayed)
	if err != nil {
		t.Fatalf("ScoreReplay frontier: %v", err)
	}
	if got.Score != 0.42 || !strings.Contains(got.Reason, "frontier") {
		t.Fatalf("frontier score = %+v", got)
	}

	failing := &scriptReplayProvider{turns: []providerTurn{{err: errors.New("down")}}}
	got, err = ScoreReplay(context.Background(), failing, original, OutcomeInfo{Success: false})
	if err != nil {
		t.Fatalf("ScoreReplay fallback should not fail hard: %v", err)
	}
	if got.Score >= 0.7 || !strings.Contains(got.Reason, "fallback") {
		t.Fatalf("fallback score = %+v", got)
	}
}

func TestScoreReplayRuleFallbackDoesNotPassNonEmptyOnly(t *testing.T) {
	original := OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "expected answer", ToolErrors: 0}
	replayed := OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "unrelated non-empty answer", ToolErrors: 0}
	got, err := ScoreReplay(context.Background(), nil, original, replayed)
	if err != nil {
		t.Fatalf("ScoreReplay rule: %v", err)
	}
	if got.Score >= 0.7 {
		t.Fatalf("rule score = %+v, want below promotion threshold for non-empty-only replay", got)
	}
}

func TestScoreReplayPromotionGradeRequiresParseableContextualScorer(t *testing.T) {
	original := OutcomeInfo{Success: true, GoalMet: true, Confidence: OutcomeConfidenceVerified, FinalAnswer: "expected answer"}
	replayed := OutcomeInfo{Success: true, GoalMet: true, Confidence: OutcomeConfidenceVerified, FinalAnswer: "provider returned text"}
	bundle := BundleV2{ID: "held-out-a", Task: "fix parser", Outcome: original}
	candidate := Candidate{Hash: "abc", Skill: validSkill("parser-helper"), SourceTask: "fix parser"}
	scorer := &scriptReplayProvider{turns: []providerTurn{{text: "looks good to me"}}}

	got, err := ScoreReplayWithContext(context.Background(), scorer, ScoreReplayRequest{
		Original:          original,
		Replayed:          replayed,
		Bundle:            bundle,
		Candidate:         candidate,
		RequireModelScore: true,
	})
	if err != nil {
		t.Fatalf("ScoreReplayWithContext: %v", err)
	}
	if got.Score >= 0.7 || !strings.Contains(got.Reason, "unparseable") {
		t.Fatalf("promotion-grade scorer fallback = %+v, want non-promotable unparseable score", got)
	}
	if len(scorer.requests) != 1 {
		t.Fatalf("scorer requests = %d, want 1", len(scorer.requests))
	}
	userPrompt := scorer.requests[0].Messages[1].Content
	if !strings.Contains(userPrompt, "Task: fix parser") || !strings.Contains(userPrompt, "Candidate skill: parser-helper") || !strings.Contains(userPrompt, "Use the parser checklist") {
		t.Fatalf("scorer prompt missing task/candidate context: %s", userPrompt)
	}
	if strings.Contains(userPrompt, "expected answer") || strings.Contains(userPrompt, "provider returned text") {
		t.Fatalf("scorer prompt leaked raw assistant answer: %s", userPrompt)
	}
}
