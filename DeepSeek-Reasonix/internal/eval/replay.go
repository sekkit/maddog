package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

type ReplayBundle struct {
	SessionID string             `json:"session_id"`
	Messages  []provider.Message `json:"messages"`
	Evidence  []evidence.Receipt `json:"evidence"`
	Outcome   OutcomeInfo        `json:"outcome"`
	Timestamp time.Time          `json:"timestamp"`
	SkillName string             `json:"skill_name,omitempty"`
}

type OutcomeInfo struct {
	Success     bool   `json:"success"`
	GoalMet     bool   `json:"goal_met"`
	FinalAnswer string `json:"final_answer,omitempty"`
	TotalTurns  int    `json:"total_turns,omitempty"`
	ToolErrors  int    `json:"tool_errors,omitempty"`
}

type CaptureOptions struct {
	SessionID string
	Session   *agent.Session
	Messages  []provider.Message
	Evidence  []evidence.Receipt
	Outcome   OutcomeInfo
	SkillName string
	Dir       string
	Now       time.Time
}

func Capture(opts CaptureOptions) (*ReplayBundle, string, error) {
	msgs := append([]provider.Message(nil), opts.Messages...)
	if len(msgs) == 0 && opts.Session != nil {
		msgs = opts.Session.Snapshot()
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	b := &ReplayBundle{
		SessionID: strings.TrimSpace(opts.SessionID),
		Messages:  msgs,
		Evidence:  append([]evidence.Receipt(nil), opts.Evidence...),
		Outcome:   opts.Outcome,
		Timestamp: opts.Now,
		SkillName: strings.TrimSpace(opts.SkillName),
	}
	if b.SessionID == "" {
		b.SessionID = fmt.Sprintf("session-%d", opts.Now.UnixNano())
	}
	if b.Outcome.FinalAnswer == "" {
		b.Outcome.FinalAnswer = lastAssistantText(msgs)
	}
	if b.Outcome.TotalTurns == 0 {
		b.Outcome.TotalTurns = countRole(msgs, provider.RoleUser)
	}
	if b.Outcome.ToolErrors == 0 {
		for _, r := range b.Evidence {
			if !r.Success {
				b.Outcome.ToolErrors++
			}
		}
	}
	if opts.Dir == "" {
		return b, "", nil
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(opts.Dir, safeFileName(b.SessionID)+"-"+opts.Now.Format("20060102T150405.000000000Z")+".json")
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, "", err
	}
	return b, path, nil
}

func LoadBundle(path string) (*ReplayBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b ReplayBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func lastAssistantText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func countRole(msgs []provider.Message, role provider.Role) int {
	n := 0
	for _, m := range msgs {
		if m.Role == role {
			n++
		}
	}
	return n
}

func safeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "session"
	}
	return out
}
