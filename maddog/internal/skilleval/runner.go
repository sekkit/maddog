package skilleval

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/agent"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type ReplayRunner struct {
	Provider  provider.Provider
	MaxTokens int
}

type AgentReplayRunner struct {
	Provider provider.Provider
	Tools    *tool.Registry
	MaxSteps int
}

func (r AgentReplayRunner) Run(ctx context.Context, bundle BundleV2, candidate Candidate) (OutcomeInfo, error) {
	if r.Provider == nil {
		return OutcomeInfo{}, fmt.Errorf("agent replay runner has no provider")
	}
	if err := validateReplayCandidate(candidate); err != nil {
		return OutcomeInfo{}, err
	}
	reg := replayToolRegistry(r.Tools, candidate)
	if reg.Len() == 0 {
		return OutcomeInfo{}, fmt.Errorf("agent replay runner has no read-only tools allowed for candidate %s", candidate.Hash)
	}
	maxSteps := r.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 8
	}
	sess := agent.NewSession(replaySystemPrompt(candidate))
	a := agent.New(r.Provider, reg, sess, agent.Options{
		MaxSteps:    maxSteps,
		Temperature: 0,
	}, event.Discard)
	if err := a.Run(ctx, replayUserPrompt(bundle)); err != nil {
		return OutcomeInfo{
			Success:          false,
			GoalMet:          false,
			Confidence:       OutcomeConfidenceVerified,
			ConfidenceReason: "agent replay returned an error",
			TotalTurns:       countAssistantTurns(sess.Snapshot()),
			ToolErrors:       countReceiptErrors(a.EvidenceReceipts()),
		}, err
	}
	msgs := sess.Snapshot()
	usageTokens := 0
	if usage := a.LastUsage(); usage != nil {
		usageTokens = usage.TotalTokens
		if usageTokens == 0 {
			usageTokens = usage.PromptTokens + usage.CompletionTokens
		}
	}
	return OutcomeInfo{
		Success:          false,
		GoalMet:          false,
		Confidence:       OutcomeConfidenceUnverified,
		ConfidenceReason: "agent replay completion is unverified and scored separately",
		FinalAnswer:      lastAssistantText(msgs),
		TotalTurns:       countAssistantTurns(msgs),
		ToolErrors:       countReceiptErrors(a.EvidenceReceipts()),
		Tokens:           usageTokens,
	}, nil
}

func (r ReplayRunner) Run(ctx context.Context, bundle BundleV2, candidate Candidate) (OutcomeInfo, error) {
	if r.Provider == nil {
		return OutcomeInfo{}, fmt.Errorf("replay runner has no provider")
	}
	if err := validateReplayCandidate(candidate); err != nil {
		return OutcomeInfo{}, err
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
		Temperature: provider.TemperaturePtr(0),
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return OutcomeInfo{Success: false, GoalMet: false, Confidence: OutcomeConfidenceVerified, ConfidenceReason: "provider replay returned an error"}, err
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return OutcomeInfo{Success: false, GoalMet: false, Confidence: OutcomeConfidenceVerified, ConfidenceReason: "provider replay returned an error chunk"}, chunk.Err
			}
		}
	}
	answer := strings.TrimSpace(text.String())
	return OutcomeInfo{
		Success:          false,
		GoalMet:          false,
		Confidence:       OutcomeConfidenceUnverified,
		ConfidenceReason: "provider replay completion is unverified and scored separately",
		FinalAnswer:      answer,
		TotalTurns:       1,
	}, nil
}

func validateReplayCandidate(candidate Candidate) error {
	switch candidate.Status {
	case CandidatePending, CandidatePromoting, CandidatePromoted:
		return nil
	case CandidateRejected:
		return fmt.Errorf("candidate %s is rejected: %s", candidate.Hash, candidate.ValidationReason)
	default:
		return fmt.Errorf("candidate %s has invalid status %q", candidate.Hash, candidate.Status)
	}
}

func replayToolRegistry(src *tool.Registry, candidate Candidate) *tool.Registry {
	dst := tool.NewRegistry()
	if src == nil {
		return dst
	}
	allowed := map[string]bool{}
	for _, name := range candidate.Skill.AllowedTools {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = true
		}
	}
	for _, name := range src.Names() {
		if len(allowed) > 0 && !allowed[name] {
			continue
		}
		tl, ok := src.Get(name)
		if !ok || tl == nil || !tl.ReadOnly() {
			continue
		}
		dst.Add(tl)
	}
	return dst
}

func countAssistantTurns(msgs []provider.Message) int {
	n := 0
	for _, msg := range msgs {
		if msg.Role == provider.RoleAssistant {
			n++
		}
	}
	return n
}

func countReceiptErrors(receipts []evidence.Receipt) int {
	n := 0
	for _, receipt := range receipts {
		if !receipt.Success {
			n++
		}
	}
	return n
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
		return OutcomeInfo{Success: false, GoalMet: false, Confidence: OutcomeConfidenceUnverified, ConfidenceReason: "dry-run replay has no candidate body"}
	}
	return OutcomeInfo{
		Success:          false,
		GoalMet:          false,
		Confidence:       OutcomeConfidenceUnverified,
		ConfidenceReason: "dry-run replay does not execute a provider",
		FinalAnswer:      body,
		TotalTurns:       1,
		ToolErrors:       bundle.Outcome.ToolErrors,
	}
}

func replayUserPrompt(bundle BundleV2) string {
	var b strings.Builder
	b.WriteString("Replay captured bundle ")
	b.WriteString(strings.TrimSpace(bundle.ID))
	b.WriteString(". Solve the task from its original input. No prior assistant answer or tool output is available.\n\n")
	if dataset := strings.TrimSpace(bundle.Dataset); dataset != "" {
		b.WriteString("Dataset: ")
		b.WriteString(dataset)
		b.WriteString("\n")
	}
	if split := strings.TrimSpace(bundle.Split); split != "" {
		b.WriteString("Split: ")
		b.WriteString(split)
		b.WriteString("\n")
	}
	if caseID := strings.TrimSpace(bundle.CaseID); caseID != "" {
		b.WriteString("Case: ")
		b.WriteString(caseID)
		b.WriteString("\n")
	}
	task := strings.TrimSpace(bundle.Task)
	caseInput := strings.TrimSpace(bundle.CaseInput)
	if task == "" && caseInput == "" {
		task = firstCapturedUserInput(bundle.Messages)
	}
	if task != "" {
		b.WriteString("\nTask:\n")
		b.WriteString(task)
		b.WriteString("\n")
	}
	if caseInput != "" && caseInput != task {
		b.WriteString("\nCase input:\n")
		b.WriteString(caseInput)
		b.WriteString("\n")
	}
	if len(bundle.Evidence) > 0 {
		b.WriteString("\nPrior verification commands (arguments and outputs omitted):\n")
		for i, receipt := range bundle.Evidence {
			if i >= 20 {
				b.WriteString("- [additional commands omitted]\n")
				break
			}
			command := strings.TrimSpace(receipt.Command)
			if command == "" {
				command = strings.TrimSpace(receipt.ToolName)
			}
			if command == "" {
				continue
			}
			status := "failed"
			if receipt.Success {
				status = "succeeded"
			}
			b.WriteString("- ")
			b.WriteString(command)
			b.WriteString(" (")
			b.WriteString(status)
			b.WriteString(")\n")
		}
	}
	return b.String()
}

func firstCapturedUserInput(messages []provider.Message) string {
	for _, msg := range messages {
		if msg.Role == provider.RoleUser && strings.TrimSpace(msg.Content) != "" {
			return strings.TrimSpace(msg.Content)
		}
	}
	return ""
}
