package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"maddog/internal/event"
)

type fallbackAdvisorTool struct {
	agent *Agent
}

func (fallbackAdvisorTool) Name() string { return "advisor" }

func (fallbackAdvisorTool) Description() string {
	return "Ask Maddog's isolated advisor for a read-only second opinion. Use when stuck, facing a risky decision, or after repeated tool failures. The advisor sees curated recent context and returns a concise plan."
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
	turnRemaining, sessionRemaining := a.advisorRemaining()
	if turnRemaining <= 0 || sessionRemaining == 0 {
		return "", fmt.Errorf("advisor consultation budget exhausted")
	}
	req := AdvisorRequest{
		Reason:           "executor requested advisor",
		Question:         question,
		Context:          a.curateAdvisorContext(a.evidence.FailureSignal()),
		RemainingTurn:    turnRemaining,
		RemainingSession: sessionRemaining,
		UsesThisTurn:     a.advisorTurnUses,
		UsesThisSession:  a.advisorSessionUses,
	}
	advice, err := a.advisorRunner(ctx, req)
	if err != nil {
		a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelWarn, Text: "advisor consultation failed: " + err.Error(),
			Advisor: event.AdvisorConsultation{Reason: req.Reason, Question: req.Question, Advice: err.Error()}})
		return "", err
	}
	a.advisorTurnUses++
	a.advisorSessionUses++
	turnRemaining, sessionRemaining = a.advisorRemaining()
	payload := event.AdvisorConsultation{
		Reason:               req.Reason,
		Question:             req.Question,
		Advice:               strings.TrimSpace(advice),
		UsesThisTurn:         a.advisorTurnUses,
		UsesThisSession:      a.advisorSessionUses,
		RemainingThisTurn:    turnRemaining,
		RemainingThisSession: sessionRemaining,
		MaxUsesPerTurn:       a.advisor.MaxUsesPerTurn,
		MaxUsesPerSession:    a.advisor.MaxUsesPerSession,
	}
	a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelInfo, Text: "advisor consulted: " + req.Reason, Advisor: payload})
	return FormatAdvisorGuidance(req, advice, turnRemaining, sessionRemaining), nil
}
