package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type rawToolResultTool struct {
	agent *Agent
}

func (rawToolResultTool) Name() string { return "tool_result" }

func (rawToolResultTool) Description() string {
	return "Retrieve the raw output behind a compressed tool result reference such as raw://tool/<id>. Read-only; use when a compressed tool output says raw output is available."
}

func (rawToolResultTool) ReadOnly() bool { return true }

func (rawToolResultTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"id": {
				"type": "string",
				"description": "Provider tool call ID, or the full raw://tool/<id> reference shown in a compressed tool result."
			}
		},
		"required": ["id"]
	}`)
}

func (t rawToolResultTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	id := strings.TrimSpace(in.ID)
	id = strings.TrimPrefix(id, "raw://tool/")
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if t.agent == nil {
		return "", fmt.Errorf("tool result store is unavailable")
	}
	raw, ok := t.agent.RawToolResult(id)
	if !ok {
		return "", fmt.Errorf("raw tool result %q is unavailable", id)
	}
	return raw, nil
}
