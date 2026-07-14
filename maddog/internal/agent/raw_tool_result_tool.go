package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	minRawToolResultPageBytes     = utf8.UTFMax
	defaultRawToolResultPageBytes = 12 * 1024
	maxRawToolResultPageBytes     = 16 * 1024
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
			},
			"offset": {
				"type": "integer",
				"minimum": 0,
				"description": "Optional zero-based byte offset for the next page of a large raw result."
			},
			"limit": {
				"type": "integer",
				"minimum": 4,
				"maximum": 16384,
				"description": "Optional maximum page size in bytes. Defaults to 12288, must be at least 4, and is capped at 16384."
			}
		},
		"required": ["id"]
	}`)
}

func (t rawToolResultTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID     string `json:"id"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
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
	// Preserve the original id-only contract. The agent's normal head/tail
	// truncation continues to protect model context for a legacy large read.
	if in.Offset == nil && in.Limit == nil {
		return raw, nil
	}
	offset := 0
	if in.Offset != nil {
		offset = *in.Offset
	}
	limit := defaultRawToolResultPageBytes
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < minRawToolResultPageBytes || limit > maxRawToolResultPageBytes {
		return "", fmt.Errorf("limit must be between %d and %d bytes", minRawToolResultPageBytes, maxRawToolResultPageBytes)
	}
	if raw == "" {
		if offset != 0 {
			return "", fmt.Errorf("offset %d is outside raw tool result of 0 bytes", offset)
		}
		return "", nil
	}
	page, end, err := rawToolResultPage(raw, offset, limit)
	if err != nil {
		return "", err
	}
	if offset == 0 && end == len(raw) {
		return page, nil
	}
	continuation := "end_of_result"
	if end < len(raw) {
		continuation = fmt.Sprintf("next_offset=%d", end)
	}
	return fmt.Sprintf("[raw tool result; byte range [%d, %d) of %d; %s]\n\n%s", offset, end, len(raw), continuation, page), nil
}

func rawToolResultPage(raw string, offset, limit int) (string, int, error) {
	if offset < 0 || offset >= len(raw) {
		return "", 0, fmt.Errorf("offset %d is outside raw tool result of %d bytes", offset, len(raw))
	}
	if utf8.ValidString(raw) && offset > 0 && !utf8.RuneStart(raw[offset]) {
		return "", 0, fmt.Errorf("offset %d does not start at a UTF-8 character boundary", offset)
	}
	end := offset + limit
	if end > len(raw) {
		end = len(raw)
	}
	if utf8.ValidString(raw) && end < len(raw) {
		for end > offset && !utf8.RuneStart(raw[end]) {
			end--
		}
	}
	return raw[offset:end], end, nil
}
