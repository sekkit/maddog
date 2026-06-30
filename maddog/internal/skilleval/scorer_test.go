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
