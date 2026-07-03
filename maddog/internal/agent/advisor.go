package agent

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/evidence"
	"maddog/internal/provider"
)

// AdvisorConfig controls automatic advisor consultations inside the Go-native
// agent loop. A non-positive MaxUsesPerTurn disables automatic consultations;
// a non-positive MaxUsesPerSession means no session cap.
type AdvisorConfig struct {
	MaxUsesPerTurn     int
	MaxUsesPerSession  int
	MaxContextMessages int
	MaxContextChars    int
}

// AdvisorRequest is the self-contained prompt material passed to the advisor
// runner. The runner may be a subagent skill, a test double, or another
// provider-backed implementation.
type AdvisorRequest struct {
	Reason           string
	Question         string
	Context          string
	RemainingTurn    int
	RemainingSession int
	UsesThisTurn     int
	UsesThisSession  int
}

// AdvisorRunner executes one advisor consultation.
type AdvisorRunner func(ctx context.Context, req AdvisorRequest) (string, error)

func (a *Agent) advisorRemaining() (turnRemaining, sessionRemaining int) {
	if a.advisor.MaxUsesPerTurn <= 0 {
		return 0, 0
	}
	turnRemaining = a.advisor.MaxUsesPerTurn - a.advisorTurnUses
	if turnRemaining < 0 {
		turnRemaining = 0
	}
	if a.advisor.MaxUsesPerSession <= 0 {
		return turnRemaining, -1
	}
	sessionRemaining = a.advisor.MaxUsesPerSession - a.advisorSessionUses
	if sessionRemaining < 0 {
		sessionRemaining = 0
	}
	if sessionRemaining < turnRemaining {
		turnRemaining = sessionRemaining
	}
	return turnRemaining, sessionRemaining
}

func cloneNativeAdvisor(in *provider.NativeAdvisorConfig) *provider.NativeAdvisorConfig {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func (a *Agent) nativeAdvisorForRequest() *provider.NativeAdvisorConfig {
	if a.nativeAdvisor == nil {
		return nil
	}
	turnRemaining, sessionRemaining := a.advisorRemaining()
	if turnRemaining <= 0 || sessionRemaining == 0 {
		return nil
	}
	out := *a.nativeAdvisor
	if out.MaxUses <= 0 || out.MaxUses > turnRemaining {
		out.MaxUses = turnRemaining
	}
	if out.MaxUses <= 0 {
		return nil
	}
	return &out
}

func (a *Agent) buildAdvisorRequest(sig evidence.FailureSignal, d UpgradeDecision) AdvisorRequest {
	turnRemaining, sessionRemaining := a.advisorRemaining()
	reason := strings.TrimSpace(d.Reason)
	if reason == "" {
		reason = "frontier upgrade was selected"
	}
	return AdvisorRequest{
		Reason:           reason,
		Question:         advisorQuestion(reason, sig),
		Context:          a.curateAdvisorContext(sig),
		RemainingTurn:    turnRemaining,
		RemainingSession: sessionRemaining,
		UsesThisTurn:     a.advisorTurnUses,
		UsesThisSession:  a.advisorSessionUses,
	}
}

func advisorQuestion(reason string, sig evidence.FailureSignal) string {
	var b strings.Builder
	if sig.DifficultDecision && !strings.Contains(strings.ToLower(reason), "frontier") {
		b.WriteString("The executor is facing a difficult decision")
	} else {
		b.WriteString("The executor is about to continue on a frontier model")
	}
	b.WriteString(" because ")
	b.WriteString(reason)
	b.WriteString(".")
	if strings.TrimSpace(sig.LastErrorTool) != "" || sig.ErrorStreak > 0 || sig.HealthScore > 0 || sig.GoalAcceptanceLoop > 0 {
		b.WriteString(" Failure surface:")
		if strings.TrimSpace(sig.LastErrorTool) != "" {
			b.WriteString(" last_error_tool=")
			b.WriteString(strings.TrimSpace(sig.LastErrorTool))
			b.WriteString(";")
		}
		if sig.ErrorStreak > 0 {
			b.WriteString(fmt.Sprintf(" error_streak=%d;", sig.ErrorStreak))
		}
		if sig.ConsecutiveErrors > 0 {
			b.WriteString(fmt.Sprintf(" consecutive_errors=%d;", sig.ConsecutiveErrors))
		}
		if sig.HealthScore > 0 {
			b.WriteString(fmt.Sprintf(" health=%.0f%%;", sig.HealthScore*100))
		}
		if sig.GoalAcceptanceLoop > 0 {
			b.WriteString(fmt.Sprintf(" goal_acceptance_loops=%d;", sig.GoalAcceptanceLoop))
		}
	}
	b.WriteString(" Give a concise correction plan, identify hidden assumptions, and name the safest next action.")
	return b.String()
}

// FormatAdvisorTask turns an AdvisorRequest into the standalone task passed to
// the built-in advisor subagent skill.
func FormatAdvisorTask(req AdvisorRequest) string {
	var b strings.Builder
	b.WriteString("Automatic advisor consultation requested.\n\n")
	b.WriteString("Reason: ")
	b.WriteString(req.Reason)
	b.WriteString("\n\nQuestion:\n")
	b.WriteString(req.Question)
	b.WriteString("\n\nOutput contract:\n")
	b.WriteString("- Use 100 words or fewer.\n")
	b.WriteString("- Use numbered steps.\n")
	b.WriteString("- End with a line starting exactly `Risks:`.\n")
	b.WriteString("\n\nBudget:\n")
	b.WriteString(fmt.Sprintf("- Remaining this turn: %d\n", req.RemainingTurn))
	if req.RemainingSession >= 0 {
		b.WriteString(fmt.Sprintf("- Remaining this session: %d\n", req.RemainingSession))
	} else {
		b.WriteString("- Remaining this session: unlimited\n")
	}
	if strings.TrimSpace(req.Context) != "" {
		b.WriteString("\nCurated context:\n")
		b.WriteString(req.Context)
		b.WriteByte('\n')
	}
	return b.String()
}

func FormatAdvisorGuidance(req AdvisorRequest, advice string, remainingTurn, remainingSession int) string {
	var b strings.Builder
	b.WriteString("Advisor guidance")
	if strings.TrimSpace(req.Reason) != "" {
		b.WriteString(" (")
		b.WriteString(req.Reason)
		b.WriteString(")")
	}
	b.WriteString(":\n\n")
	b.WriteString(strings.TrimSpace(advice))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("[Advisor consultations remaining this turn: %d", remainingTurn))
	if remainingSession >= 0 {
		b.WriteString(fmt.Sprintf("; remaining this session: %d", remainingSession))
	}
	b.WriteString("]")
	return b.String()
}

func (a *Agent) curateAdvisorContext(sig evidence.FailureSignal) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Failure signal: consecutive_errors=%d, error_streak=%d, last_error_tool=%q, health_score=%.2f, goal_acceptance_loops=%d, difficult_decision=%v, decision=%q\n",
		sig.ConsecutiveErrors, sig.ErrorStreak, sig.LastErrorTool, sig.HealthScore, sig.GoalAcceptanceLoop, sig.DifficultDecision, sig.DecisionSummary))

	msgs := a.session.Messages
	maxMessages := a.advisor.MaxContextMessages
	if maxMessages > 0 && len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	for _, msg := range msgs {
		writeAdvisorMessage(&b, msg)
	}
	out := b.String()
	maxChars := a.advisor.MaxContextChars
	if maxChars > 0 && len(out) > maxChars {
		return "[older advisor context truncated]\n" + out[len(out)-maxChars:]
	}
	return out
}

func writeAdvisorMessage(b *strings.Builder, msg provider.Message) {
	role := string(msg.Role)
	if msg.Role == provider.RoleTool && msg.Name != "" {
		role += ":" + msg.Name
	}
	b.WriteString("\n[")
	b.WriteString(role)
	b.WriteString("]\n")
	if msg.Content != "" {
		b.WriteString(strings.TrimSpace(msg.Content))
		b.WriteByte('\n')
	}
	if len(msg.ToolCalls) > 0 {
		b.WriteString("tool_calls:\n")
		for _, tc := range msg.ToolCalls {
			b.WriteString("- ")
			b.WriteString(tc.Name)
			if tc.Arguments != "" {
				b.WriteString(" ")
				b.WriteString(tc.Arguments)
			}
			b.WriteByte('\n')
		}
	}
}
