package skilleval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"maddog/internal/agent"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/skill"
)

type BundleV2 struct {
	Version   int                `json:"version"`
	ID        string             `json:"id"`
	Path      string             `json:"path,omitempty"`
	SessionID string             `json:"session_id"`
	Task      string             `json:"task,omitempty"`
	SkillName string             `json:"skill_name,omitempty"`
	Skills    []SkillSnapshot    `json:"skills,omitempty"`
	Dynamic   *SkillSnapshot     `json:"dynamic_skill,omitempty"`
	Messages  []provider.Message `json:"messages"`
	Evidence  []evidence.Receipt `json:"evidence"`
	History   []HistoryItem      `json:"history,omitempty"`
	Metrics   BundleMetrics      `json:"metrics,omitempty"`
	Review    HumanReview        `json:"human_review,omitempty"`
	Outcome   OutcomeInfo        `json:"outcome"`
	CreatedAt time.Time          `json:"created_at"`
}

type SkillSnapshot struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Body         string   `json:"body,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	RunAs        string   `json:"run_as,omitempty"`
	Model        string   `json:"model,omitempty"`
	Effort       string   `json:"effort,omitempty"`
}

type HistoryItem struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type BundleMetrics struct {
	Compressed []CompressionMetric `json:"compressed,omitempty"`
}

type CompressionMetric struct {
	Name            string `json:"name"`
	OriginalBytes   int    `json:"original_bytes,omitempty"`
	CompressedBytes int    `json:"compressed_bytes,omitempty"`
	TokenDelta      int    `json:"token_delta,omitempty"`
}

type HumanReview struct {
	Approved bool      `json:"approved,omitempty"`
	Denied   bool      `json:"denied,omitempty"`
	Reviewer string    `json:"reviewer,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	At       time.Time `json:"at,omitempty"`
}

type OutcomeInfo struct {
	Success          bool              `json:"success"`
	GoalMet          bool              `json:"goal_met"`
	Confidence       OutcomeConfidence `json:"confidence,omitempty"`
	ConfidenceReason string            `json:"confidence_reason,omitempty"`
	FinalAnswer      string            `json:"final_answer,omitempty"`
	TotalTurns       int               `json:"total_turns,omitempty"`
	ToolErrors       int               `json:"tool_errors,omitempty"`
	Tokens           int               `json:"tokens,omitempty"`
	AdvisorUses      int               `json:"advisor_uses,omitempty"`
	HumanReviews     int               `json:"human_reviews,omitempty"`
}

type OutcomeConfidence string

const (
	OutcomeConfidenceUnknown    OutcomeConfidence = "unknown"
	OutcomeConfidenceUnverified OutcomeConfidence = "unverified"
	OutcomeConfidenceVerified   OutcomeConfidence = "verified"
)

type CaptureOptions struct {
	SessionID string
	Task      string
	SkillName string
	Skills    []skill.Skill
	Dynamic   *skill.Skill
	Session   *agent.Session
	Messages  []provider.Message
	Evidence  []evidence.Receipt
	History   []HistoryItem
	Metrics   BundleMetrics
	Review    HumanReview
	Outcome   OutcomeInfo
	Dir       string
	Now       time.Time
}

func CaptureBundle(opts CaptureOptions) (*BundleV2, string, error) {
	msgs := append([]provider.Message(nil), opts.Messages...)
	if len(msgs) == 0 && opts.Session != nil {
		msgs = opts.Session.Snapshot()
	}
	msgs = nonSystemMessages(msgs)
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	sessionID := strings.TrimSpace(opts.SessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", now.UnixNano())
	}
	b := &BundleV2{
		Version:   2,
		SessionID: sessionID,
		Task:      strings.TrimSpace(opts.Task),
		SkillName: strings.TrimSpace(opts.SkillName),
		Skills:    snapshotSkills(opts.Skills),
		Dynamic:   snapshotSkillPtr(opts.Dynamic),
		Messages:  msgs,
		Evidence:  cloneReceipts(opts.Evidence),
		History:   cloneHistory(opts.History),
		Metrics:   cloneMetrics(opts.Metrics),
		Review:    opts.Review,
		Outcome:   opts.Outcome,
		CreatedAt: now,
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
	if b.Outcome.Confidence == "" {
		b.Outcome.Confidence = OutcomeConfidenceUnverified
		if b.Outcome.ConfidenceReason == "" {
			b.Outcome.ConfidenceReason = "outcome was captured without a verified completion signal"
		}
	}
	b.ID = bundleID(*b)
	if opts.Dir == "" {
		return b, "", nil
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, "", err
	}
	path := uniqueBundlePath(opts.Dir, safeFileName(b.SessionID), now)
	b.Path = path
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, "", err
	}
	return b, path, nil
}

func LoadBundle(path string) (*BundleV2, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b BundleV2
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func bundleID(b BundleV2) string {
	b.ID = ""
	b.Path = ""
	data, err := json.Marshal(b)
	if err != nil {
		return fmt.Sprintf("bundle-%d", b.CreatedAt.UnixNano())
	}
	sum := sha256.Sum256(data)
	return "bundle-" + hex.EncodeToString(sum[:8])
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
	var count int
	for _, msg := range msgs {
		if msg.Role == role {
			count++
		}
	}
	return count
}

func cloneReceipts(receipts []evidence.Receipt) []evidence.Receipt {
	out := append([]evidence.Receipt(nil), receipts...)
	for i := range out {
		out[i].Args = append([]byte(nil), out[i].Args...)
		out[i].Paths = append([]string(nil), out[i].Paths...)
		out[i].Todos = append([]evidence.TodoItem(nil), out[i].Todos...)
		if out[i].TodoStep != nil {
			step := *out[i].TodoStep
			out[i].TodoStep = &step
		}
	}
	return out
}

func cloneHistory(history []HistoryItem) []HistoryItem {
	return append([]HistoryItem(nil), history...)
}

func snapshotSkills(skills []skill.Skill) []SkillSnapshot {
	if len(skills) == 0 {
		return nil
	}
	out := make([]SkillSnapshot, 0, len(skills))
	for _, sk := range skills {
		out = append(out, snapshotSkill(sk))
	}
	return out
}

func snapshotSkillPtr(sk *skill.Skill) *SkillSnapshot {
	if sk == nil {
		return nil
	}
	snap := snapshotSkill(*sk)
	return &snap
}

func snapshotSkill(sk skill.Skill) SkillSnapshot {
	return SkillSnapshot{
		Name:         strings.TrimSpace(sk.Name),
		Description:  strings.TrimSpace(sk.Description),
		Body:         strings.TrimSpace(sk.Body),
		AllowedTools: append([]string(nil), sk.AllowedTools...),
		RunAs:        string(sk.RunAs),
		Model:        strings.TrimSpace(sk.Model),
		Effort:       strings.TrimSpace(sk.Effort),
	}
}

func cloneMetrics(metrics BundleMetrics) BundleMetrics {
	return BundleMetrics{Compressed: append([]CompressionMetric(nil), metrics.Compressed...)}
}

func nonSystemMessages(msgs []provider.Message) []provider.Message {
	out := make([]provider.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Role != provider.RoleSystem {
			out = append(out, msg)
		}
	}
	return out
}

func uniqueBundlePath(dir, base string, now time.Time) string {
	first := filepath.Join(dir, base+".json")
	if _, err := os.Stat(first); os.IsNotExist(err) {
		return first
	}
	stamp := now.Format("20060102T150405.000000000Z")
	for i := 0; ; i++ {
		name := base + "-" + stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", name, i)
		}
		path := filepath.Join(dir, name+".json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
	}
}

func safeFileName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		valid := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.'
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "session"
	}
	return name
}
