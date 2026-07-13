package skilleval

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"maddog/internal/provider"
)

type ScoreResult struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

type ScoreReplayRequest struct {
	Original          OutcomeInfo
	Replayed          OutcomeInfo
	Bundle            BundleV2
	Candidate         Candidate
	RequireModelScore bool
}

func ScoreReplay(ctx context.Context, frontier provider.Provider, original, replayed OutcomeInfo) (ScoreResult, error) {
	return ScoreReplayWithContext(ctx, frontier, ScoreReplayRequest{Original: original, Replayed: replayed})
}

func ScoreReplayWithContext(ctx context.Context, frontier provider.Provider, req ScoreReplayRequest) (ScoreResult, error) {
	if frontier == nil {
		if req.RequireModelScore {
			return ScoreResult{Score: 0, Reason: "promotion-grade scoring requires a model scorer"}, nil
		}
		return ruleScore(req.Original, req.Replayed), nil
	}
	ch, err := frontier.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "Score the replayed skill-eval outcome against the original from 0.0 to 1.0. Return a number first, then a short reason."},
			{Role: provider.RoleUser, Content: scoreReplayPrompt(req)},
		},
		Temperature: 0,
		MaxTokens:   200,
	})
	if err != nil {
		return scoreReplayFallback(req, "scorer error: "+err.Error()), nil
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return scoreReplayFallback(req, "scorer stream error: "+chunk.Err.Error()), nil
			}
		}
	}
	raw := strings.TrimSpace(text.String())
	score, ok := leadingScore(raw)
	if !ok {
		return scoreReplayFallback(req, "unparseable scorer output"), nil
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return ScoreResult{Score: score, Reason: raw}, nil
}

func scoreReplayFallback(req ScoreReplayRequest, reason string) ScoreResult {
	if req.RequireModelScore {
		return ScoreResult{Score: 0, Reason: "promotion-grade scorer " + reason}
	}
	rs := ruleScore(req.Original, req.Replayed)
	rs.Reason = "rule fallback after " + reason
	return rs
}

func scoreReplayPrompt(req ScoreReplayRequest) string {
	var b strings.Builder
	if strings.TrimSpace(req.Bundle.ID) != "" {
		b.WriteString("Bundle: ")
		b.WriteString(strings.TrimSpace(req.Bundle.ID))
		b.WriteString("\n")
	}
	task := strings.TrimSpace(firstNonEmptyString(req.Bundle.Task, req.Candidate.SourceTask))
	if task != "" {
		b.WriteString("Task: ")
		b.WriteString(task)
		b.WriteString("\n")
	}
	if strings.TrimSpace(req.Candidate.Skill.Name) != "" {
		b.WriteString("Candidate skill: ")
		b.WriteString(strings.TrimSpace(req.Candidate.Skill.Name))
		b.WriteString("\n")
	}
	if body := strings.TrimSpace(req.Candidate.Skill.Body); body != "" {
		b.WriteString("Candidate skill body:\n")
		b.WriteString(trimForScorePrompt(body, 4000))
		b.WriteString("\n")
	}
	b.WriteString("\nOriginal outcome:\n")
	b.WriteString(outcomeForScorePrompt(req.Original))
	b.WriteString("\nReplayed outcome:\n")
	b.WriteString(outcomeForScorePrompt(req.Replayed))
	b.WriteString("\nScore whether the replayed answer satisfies the task at least as well as the original, using the task and candidate skill context above.")
	return b.String()
}

func outcomeForScorePrompt(out OutcomeInfo) string {
	// Never put the original or replayed assistant answer in the scorer
	// context. A scorer that sees the incumbent answer can simply copy or
	// reward it, leaking held-out solution content into the optimization loop.
	// Presence and length are sufficient diagnostics while verifier signals stay
	// available for an independent scorer.
	answer := strings.TrimSpace(out.FinalAnswer)
	return fmt.Sprintf("success=%v goal=%v confidence=%s tool_errors=%d tokens=%d answer_present=%v answer_length=%d\n", out.Success, out.GoalMet, out.Confidence, out.ToolErrors, out.Tokens, answer != "", len(answer))
}

func trimForScorePrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n[truncated]"
}

func ruleScore(original, replayed OutcomeInfo) ScoreResult {
	score := 0.1
	switch {
	case replayed.Success && replayed.GoalMet && original.Success && original.GoalMet:
		score = 0.45
	case replayed.Success && replayed.GoalMet:
		score = 0.35
	case replayed.Success:
		score = 0.25
	case !replayed.Success:
		score = 0.1
	}
	if original.Success && !replayed.Success {
		score = 0.1
	}
	if replayed.ToolErrors < original.ToolErrors {
		score += 0.1
	}
	if original.Tokens > 0 && replayed.Tokens > 0 && replayed.Tokens < original.Tokens {
		score += 0.05
	}
	if original.AdvisorUses > 0 && replayed.AdvisorUses < original.AdvisorUses {
		score += 0.05
	}
	if original.HumanReviews > 0 && replayed.HumanReviews < original.HumanReviews {
		score += 0.05
	}
	if strings.TrimSpace(replayed.FinalAnswer) != "" && strings.TrimSpace(replayed.FinalAnswer) == strings.TrimSpace(original.FinalAnswer) {
		score += 0.35
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
