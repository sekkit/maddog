package eval

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/agent"
	"maddog/internal/event"
	"maddog/internal/provider"
	"maddog/internal/skill"
	"maddog/internal/tool"
)

type ReplayRunner struct {
	Provider provider.Provider
	Registry *tool.Registry
	MaxSteps int
}

func (r ReplayRunner) Run(ctx context.Context, bundle ReplayBundle, sk skill.Skill) (OutcomeInfo, error) {
	if r.Provider == nil {
		return OutcomeInfo{}, fmt.Errorf("replay runner has no provider")
	}
	prompt := replayPrompt(bundle)
	sys := strings.TrimSpace(sk.Body)
	if sys == "" {
		sys = "Replay the task and produce the best final answer."
	}
	reg := r.Registry
	if reg == nil {
		reg = tool.NewRegistry()
	}
	steps := r.MaxSteps
	if steps == 0 {
		steps = 6
	}
	answer, err := agent.RunSubAgentWithSession(ctx, r.Provider, reg, agent.NewSession(sys), prompt, agent.Options{MaxSteps: steps}, event.Discard)
	if err != nil {
		return OutcomeInfo{Success: false, GoalMet: false}, err
	}
	return OutcomeInfo{
		Success:     strings.TrimSpace(answer) != "",
		GoalMet:     strings.TrimSpace(answer) != "",
		FinalAnswer: answer,
		TotalTurns:  1,
	}, nil
}

func replayPrompt(bundle ReplayBundle) string {
	var b strings.Builder
	b.WriteString("Replay this captured developer task. Use the prior transcript and evidence only as context; produce the best final answer for the task.\n\n")
	for _, m := range bundle.Messages {
		if m.Role == provider.RoleSystem {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		b.WriteString(strings.ToUpper(string(m.Role)))
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
			if r.Command != "" {
				b.WriteString(": ")
				b.WriteString(r.Command)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
