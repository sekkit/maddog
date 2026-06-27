package eval

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/provider"
)

type ScoreResult struct {
	Score  float64
	Reason string
}

func Score(ctx context.Context, frontier provider.Provider, original, replayed OutcomeInfo) (ScoreResult, error) {
	if frontier == nil {
		return ruleScore(original, replayed), nil
	}
	ch, err := frontier.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "Score the replayed outcome against the original from 0.0 to 1.0. Return a number first, then a short reason."},
			{Role: provider.RoleUser, Content: fmt.Sprintf("Original success=%v goal=%v answer=%q\nReplayed success=%v goal=%v answer=%q", original.Success, original.GoalMet, original.FinalAnswer, replayed.Success, replayed.GoalMet, replayed.FinalAnswer)},
		},
		Temperature: 0,
		MaxTokens:   200,
	})
	if err != nil {
		rs := ruleScore(original, replayed)
		rs.Reason = "rule fallback after scorer error: " + err.Error()
		return rs, err
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			rs := ruleScore(original, replayed)
			rs.Reason = "rule fallback after scorer stream error: " + chunk.Err.Error()
			return rs, chunk.Err
		}
	}
	raw := strings.TrimSpace(text.String())
	score, ok := leadingScore(raw)
	if !ok {
		rs := ruleScore(original, replayed)
		rs.Reason = "rule fallback after unparseable scorer output"
		return rs, fmt.Errorf("unparseable scorer output %q", raw)
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return ScoreResult{Score: score, Reason: raw}, nil
}

func ruleScore(original, replayed OutcomeInfo) ScoreResult {
	score := 0.5
	switch {
	case replayed.Success && replayed.GoalMet:
		score = 0.8
	case replayed.Success:
		score = 0.7
	case !replayed.Success:
		score = 0.2
	}
	if original.Success && !replayed.Success {
		score = 0.1
	}
	if replayed.ToolErrors < original.ToolErrors {
		score += 0.05
	}
	if strings.TrimSpace(replayed.FinalAnswer) != "" && strings.TrimSpace(replayed.FinalAnswer) == strings.TrimSpace(original.FinalAnswer) {
		score += 0.1
	}
	if score > 1 {
		score = 1
	}
	return ScoreResult{Score: score, Reason: "rule score"}
}

func leadingScore(s string) (float64, bool) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return 0, false
	}
	token := strings.Trim(fields[0], " \t\r\n:")
	token = strings.TrimSuffix(token, "/1")
	v, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, false
	}
	if v > 1 && v <= 100 {
		v = v / 100
	}
	return v, true
}
