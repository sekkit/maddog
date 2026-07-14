package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type fallbackAdvisorTool struct {
	agent *Agent
}

func (fallbackAdvisorTool) Name() string { return "advisor" }

func (fallbackAdvisorTool) Description() string {
	return "Ask Maddog's isolated advisor for a concise read-only second opinion. For complex or high-risk work, consult after initial read-only orientation and before substantive writes; also consult when stuck, changing approach, or reviewing completion when budget remains. Skip simple tasks."
}

func (fallbackAdvisorTool) ReadOnly() bool { return true }

func (fallbackAdvisorTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"question": {
				"type": "string",
				"description": "Specific question for the advisor. Name the uncertainty, failure, or decision you need help with."
			}
		},
		"required": ["question"]
	}`)
}

func (t fallbackAdvisorTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	question := strings.TrimSpace(in.Question)
	if question == "" {
		return "", fmt.Errorf("question is required")
	}
	a := t.agent
	if a == nil || a.advisorRunner == nil {
		return "", fmt.Errorf("advisor is not configured")
	}
	if a.advisorSuppressed {
		return "", fmt.Errorf("advisor is disabled for this turn by the user's request")
	}
	turnRemaining, sessionRemaining := a.advisorRemaining()
	if turnRemaining <= 0 || sessionRemaining == 0 {
		return "", fmt.Errorf("advisor consultation budget exhausted")
	}
	req := AdvisorRequest{
		Reason:           "executor requested advisor",
		Question:         question,
		Context:          a.curateAdvisorContext(a.evidence.FailureSignal()),
		Domain:           a.inferAdvisorDomain(question, a.evidence.FailureSignal()),
		RemainingTurn:    turnRemaining,
		RemainingSession: sessionRemaining,
		UsesThisTurn:     a.advisorTurnUses,
		UsesThisSession:  a.advisorSessionUses,
	}
	advice, err := a.invokeFallbackAdvisor(ctx, req)
	if err != nil {
		return "", err
	}
	turnRemaining, sessionRemaining = a.advisorRemaining()
	return FormatAdvisorGuidance(req, advice, turnRemaining, sessionRemaining), nil
}
