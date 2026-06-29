package skilleval

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/provider"
)

type ReplayRunner struct {
	Provider  provider.Provider
	MaxTokens int
}

func (r ReplayRunner) Run(ctx context.Context, bundle BundleV2, candidate Candidate) (OutcomeInfo, error) {
	if r.Provider == nil {
		return OutcomeInfo{}, fmt.Errorf("replay runner has no provider")
	}
	switch candidate.Status {
	case CandidatePending, CandidatePromoting, CandidatePromoted:
	case CandidateRejected:
		return OutcomeInfo{}, fmt.Errorf("candidate %s is rejected: %s", candidate.Hash, candidate.ValidationReason)
	default:
		return OutcomeInfo{}, fmt.Errorf("candidate %s has invalid status %q", candidate.Hash, candidate.Status)
	}
	maxTokens := r.MaxTokens
	if maxTokens == 0 {
		maxTokens = 800
	}
	ch, err := r.Provider.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: replaySystemPrompt(candidate)},
			{Role: provider.RoleUser, Content: replayUserPrompt(bundle)},
		},
		Temperature: 0,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return OutcomeInfo{Success: false, GoalMet: false}, err
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return OutcomeInfo{Success: false, GoalMet: false}, chunk.Err
			}
		}
	}
	answer := strings.TrimSpace(text.String())
	return OutcomeInfo{
		Success:     answer != "",
		GoalMet:     answer != "",
		FinalAnswer: answer,
		TotalTurns:  1,
	}, nil
}

func replaySystemPrompt(candidate Candidate) string {
	body := strings.TrimSpace(candidate.Skill.Body)
	if body == "" {
		body = "Replay the task and produce the best final answer."
	}
	return "You are running a read-only replay for skill evaluation. Do not call destructive tools or mutate files.\n\nCandidate skill:\n" + body
}

func DryRunReplay(bundle BundleV2, candidate Candidate) OutcomeInfo {
	body := strings.TrimSpace(candidate.Skill.Body)
	if body == "" {
		return OutcomeInfo{Success: false, GoalMet: false}
	}
	return OutcomeInfo{
		Success:     true,
		GoalMet:     bundle.Outcome.GoalMet || bundle.Outcome.Success,
		FinalAnswer: body,
		TotalTurns:  1,
		ToolErrors:  bundle.Outcome.ToolErrors,
	}
}

func replayUserPrompt(bundle BundleV2) string {
	var b strings.Builder
	b.WriteString("Replay captured bundle ")
	b.WriteString(strings.TrimSpace(bundle.ID))
	b.WriteString(". Use transcript, evidence, and metadata as context; produce the best final answer for the task.\n\n")
	if strings.TrimSpace(bundle.Task) != "" {
		b.WriteString("Task: ")
		b.WriteString(strings.TrimSpace(bundle.Task))
		b.WriteString("\n\n")
	}
	for _, msg := range bundle.Messages {
		if msg.Role == provider.RoleSystem {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		b.WriteString(strings.ToUpper(string(msg.Role)))
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	if len(bundle.Evidence) > 0 {
		b.WriteString("Evidence receipts:\n")
		for _, r := range bundle.Evidence {
			status := "failed"
			if r.Success {
				status = "succeeded"
			}
			b.WriteString("- ")
			b.WriteString(r.ToolName)
			b.WriteString(" ")
			b.WriteString(status)
			if strings.TrimSpace(r.Command) != "" {
				b.WriteString(": ")
				b.WriteString(strings.TrimSpace(r.Command))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
