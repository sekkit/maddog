package skilleval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"maddog/internal/agent"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/skill"
)

type BundleV2 struct {
	Version                int                `json:"version"`
	ID                     string             `json:"id"`
	Path                   string             `json:"path,omitempty"`
	SessionID              string             `json:"session_id"`
	Task                   string             `json:"task,omitempty"`
	CaseInput              string             `json:"case_input,omitempty"`
	Dataset                string             `json:"dataset,omitempty"`
	Split                  string             `json:"split,omitempty"`
	CaseID                 string             `json:"case_id,omitempty"`
	Seed                   int64              `json:"seed"`
	VerifierFingerprint    string             `json:"verifier_fingerprint,omitempty"`
	EnvironmentFingerprint string             `json:"environment_fingerprint,omitempty"`
	ProviderFingerprint    string             `json:"provider_fingerprint,omitempty"`
	SkillName              string             `json:"skill_name,omitempty"`
	UsedSkillNames         []string           `json:"used_skill_names,omitempty"`
	Skills                 []SkillSnapshot    `json:"skills,omitempty"`
	Dynamic                *SkillSnapshot     `json:"dynamic_skill,omitempty"`
	Messages               []provider.Message `json:"messages"`
	Evidence               []evidence.Receipt `json:"evidence"`
	History                []HistoryItem      `json:"history,omitempty"`
	Metrics                BundleMetrics      `json:"metrics,omitempty"`
	Review                 HumanReview        `json:"human_review,omitempty"`
	Outcome                OutcomeInfo        `json:"outcome"`
	CreatedAt              time.Time          `json:"created_at"`
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
	SessionID              string
	Task                   string
	CaseInput              string
	Dataset                string
	Split                  string
	CaseID                 string
	Seed                   int64
	VerifierFingerprint    string
	EnvironmentFingerprint string
	ProviderFingerprint    string
	SkillName              string
	UsedSkillNames         []string
	Skills                 []skill.Skill
	Dynamic                *skill.Skill
	Session                *agent.Session
	Messages               []provider.Message
	Evidence               []evidence.Receipt
	History                []HistoryItem
	Metrics                BundleMetrics
	Review                 HumanReview
	Outcome                OutcomeInfo
	Sanitization           *CaptureSanitizationPolicy
	Retention              *BundleRetentionPolicy
	Dir                    string
	Now                    time.Time
}

// CaptureSanitizationPolicy controls which potentially sensitive replay data
// is persisted. A nil policy preserves the legacy capture behavior so existing
// callers keep working. Pass SafeCaptureSanitizationPolicy for the restrictive
// policy used by new automatic capture paths.
type CaptureSanitizationPolicy struct {
	IncludeReasoning          bool
	IncludeImages             bool
	IncludeNativeBlocks       bool
	IncludeToolArguments      bool
	IncludeToolResults        bool
	IncludeEvidenceArguments  bool
	IncludeUnrelatedSkills    bool
	IncludeUnrelatedSkillBody bool
	// RedactSecrets removes common credential assignments and data-URL image
	// payloads from every persisted text field. It is enabled by the safe
	// policy and intentionally separate from field inclusion flags.
	RedactSecrets bool
}

// BundleRetentionPolicy bounds persisted replay artifacts. A zero value leaves
// the corresponding bound disabled. Cleanup never follows symlinks and only
// removes regular JSON bundle files in the supplied directory.
type BundleRetentionPolicy struct {
	MaxAge   time.Duration
	MaxCount int
	Now      time.Time
}

func SafeCaptureSanitizationPolicy() *CaptureSanitizationPolicy {
	return &CaptureSanitizationPolicy{RedactSecrets: true}
}

func LegacyCaptureSanitizationPolicy() CaptureSanitizationPolicy {
	return CaptureSanitizationPolicy{
		IncludeReasoning:          true,
		IncludeImages:             true,
		IncludeNativeBlocks:       true,
		IncludeToolArguments:      true,
		IncludeToolResults:        true,
		IncludeEvidenceArguments:  true,
		IncludeUnrelatedSkills:    true,
		IncludeUnrelatedSkillBody: true,
		RedactSecrets:             false,
	}
}

func CaptureBundle(opts CaptureOptions) (*BundleV2, string, error) {
	policy := LegacyCaptureSanitizationPolicy()
	if opts.Sanitization != nil {
		policy = *opts.Sanitization
	}
	msgs := cloneMessages(opts.Messages)
	if len(msgs) == 0 && opts.Session != nil {
		msgs = cloneMessages(opts.Session.Snapshot())
	}
	msgs = sanitizeMessages(nonSystemMessages(msgs), policy)
	usedSkillNames := captureUsedSkillNames(opts)
	evidenceReceipts := sanitizeReceipts(opts.Evidence, policy)
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
		Version:                2,
		SessionID:              sessionID,
		Task:                   strings.TrimSpace(opts.Task),
		CaseInput:              strings.TrimSpace(opts.CaseInput),
		Dataset:                strings.TrimSpace(opts.Dataset),
		Split:                  strings.TrimSpace(opts.Split),
		CaseID:                 strings.TrimSpace(opts.CaseID),
		Seed:                   opts.Seed,
		VerifierFingerprint:    strings.TrimSpace(opts.VerifierFingerprint),
		EnvironmentFingerprint: strings.TrimSpace(opts.EnvironmentFingerprint),
		ProviderFingerprint:    strings.TrimSpace(opts.ProviderFingerprint),
		SkillName:              strings.TrimSpace(opts.SkillName),
		UsedSkillNames:         usedSkillNames,
		Skills:                 snapshotSkillsForCapture(opts.Skills, usedSkillNames, policy),
		Dynamic:                snapshotDynamicSkillForCapture(opts.Dynamic, usedSkillNames, policy),
		Messages:               msgs,
		Evidence:               evidenceReceipts,
		History:                cloneHistory(opts.History),
		Metrics:                cloneMetrics(opts.Metrics),
		Review:                 opts.Review,
		Outcome:                opts.Outcome,
		CreatedAt:              now,
	}
	if policy.RedactSecrets {
		sanitizeBundleSecrets(b)
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
	if reason := verifiedOutcomeEvidenceReason(b.Evidence, b.Review); reason != "" && strings.TrimSpace(b.Outcome.FinalAnswer) != "" {
		b.Outcome.Success = true
		b.Outcome.GoalMet = true
		b.Outcome.Confidence = OutcomeConfidenceVerified
		b.Outcome.ConfidenceReason = reason
		if b.Review.Approved && b.Outcome.HumanReviews == 0 {
			b.Outcome.HumanReviews = 1
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
	if opts.Retention != nil {
		if _, err := CleanupBundles(opts.Dir, *opts.Retention); err != nil {
			return nil, "", err
		}
	}
	return b, path, nil
}

func verifiedOutcomeEvidenceReason(receipts []evidence.Receipt, review HumanReview) string {
	if review.Approved && !review.Denied {
		if strings.TrimSpace(review.Reason) != "" {
			return "verified by human review: " + strings.TrimSpace(review.Reason)
		}
		return "verified by human review"
	}
	var lastStepProof string
	var lastVerification *evidence.Receipt
	for _, receipt := range receipts {
		if receipt.StepProof {
			if receipt.Success {
				step := strings.TrimSpace(receipt.Step)
				if step != "" {
					lastStepProof = "verified by completed step: " + step
				} else {
					lastStepProof = "verified by completed step"
				}
			}
		}
		if isVerificationCommand(receipt.ToolName, receipt.Command) {
			r := receipt
			lastVerification = &r
		}
	}
	if lastVerification != nil {
		if !lastVerification.Success {
			return ""
		}
		cmd := strings.TrimSpace(lastVerification.Command)
		if cmd == "" {
			cmd = strings.TrimSpace(lastVerification.ToolName)
		}
		return "verified by command: " + cmd
	}
	return lastStepProof
}

func isVerificationCommand(toolName, command string) bool {
	text := strings.ToLower(strings.TrimSpace(command))
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(toolName))
	}
	if text == "" {
		return false
	}
	prefixes := []string{
		"go test",
		"npm test", "npm run test",
		"pnpm test", "pnpm run test",
		"yarn test",
		"pytest", "python -m pytest", "python3 -m pytest",
		"cargo test",
		"mvn test", "gradle test", "./gradlew test", "make test",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return strings.Contains(text, " run test") ||
		strings.Contains(text, " run test:")
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

func ApplyHumanReview(b *BundleV2, review HumanReview) {
	if b == nil {
		return
	}
	if review.At.IsZero() {
		review.At = time.Now().UTC()
	} else {
		review.At = review.At.UTC()
	}
	b.Review = review
	if review.Approved && !review.Denied && strings.TrimSpace(b.Outcome.FinalAnswer) != "" {
		b.Outcome.Success = true
		b.Outcome.GoalMet = true
		b.Outcome.Confidence = OutcomeConfidenceVerified
		b.Outcome.ConfidenceReason = verifiedOutcomeEvidenceReason(b.Evidence, review)
		if b.Outcome.ConfidenceReason == "" {
			b.Outcome.ConfidenceReason = "verified by human review"
		}
		b.Outcome.HumanReviews = 1
	} else if review.Denied {
		b.Outcome.Success = false
		b.Outcome.GoalMet = false
		b.Outcome.Confidence = OutcomeConfidenceUnverified
		reason := strings.TrimSpace(review.Reason)
		if reason == "" {
			reason = "denied by human review"
		} else {
			reason = "denied by human review: " + reason
		}
		b.Outcome.ConfidenceReason = reason
		b.Outcome.HumanReviews = 1
	}
	b.ID = bundleID(*b)
}

func SaveBundle(path string, b *BundleV2) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("bundle path is required")
	}
	if b == nil {
		return fmt.Errorf("bundle is nil")
	}
	b.Path = path
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
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

func sanitizeReceipts(receipts []evidence.Receipt, policy CaptureSanitizationPolicy) []evidence.Receipt {
	out := cloneReceipts(receipts)
	if policy.IncludeEvidenceArguments {
		return out
	}
	for i := range out {
		out[i].Args = nil
	}
	return out
}

func cloneMessages(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]provider.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].Images = append([]string(nil), msg.Images...)
		out[i].ToolCalls = append([]provider.ToolCall(nil), msg.ToolCalls...)
		out[i].MemoryCitations = append([]provider.MemoryCitation(nil), msg.MemoryCitations...)
		if len(msg.NativeBlocks) > 0 {
			out[i].NativeBlocks = make([]json.RawMessage, len(msg.NativeBlocks))
			for j, block := range msg.NativeBlocks {
				out[i].NativeBlocks[j] = append(json.RawMessage(nil), block...)
			}
		}
	}
	return out
}

func sanitizeMessages(messages []provider.Message, policy CaptureSanitizationPolicy) []provider.Message {
	out := make([]provider.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == provider.RoleTool && !policy.IncludeToolResults {
			continue
		}
		if !policy.IncludeReasoning {
			msg.ReasoningContent = ""
			msg.ReasoningSignature = ""
		}
		if !policy.IncludeImages {
			msg.Images = nil
		}
		if !policy.IncludeNativeBlocks {
			msg.NativeBlocks = nil
		}
		if !policy.IncludeToolArguments {
			for i := range msg.ToolCalls {
				msg.ToolCalls[i].Arguments = ""
				msg.ToolCalls[i].Diff = ""
				msg.ToolCalls[i].Added = 0
				msg.ToolCalls[i].Removed = 0
			}
		}
		out = append(out, msg)
	}
	return out
}

var (
	credentialAssignmentPattern = regexp.MustCompile(`(?i)(\b(?:api[_-]?key|access[_-]?token|auth(?:orization)?|password|passwd|secret|token|private[_-]?key)\b\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
	bearerCredentialPattern     = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`)
	commonSecretTokenPattern    = regexp.MustCompile(`\b(?:sk|ghp|github_pat|xox[baprs])[-_][A-Za-z0-9_-]{12,}\b`)
	imageDataURLPattern         = regexp.MustCompile(`(?i)data:image/[a-z0-9.+-]+;base64,[a-z0-9+/=\r\n]+`)
)

func sanitizeBundleSecrets(bundle *BundleV2) {
	if bundle == nil {
		return
	}
	bundle.Task = redactSensitiveText(bundle.Task)
	bundle.CaseInput = redactSensitiveText(bundle.CaseInput)
	bundle.ProviderFingerprint = redactSensitiveText(bundle.ProviderFingerprint)
	for i := range bundle.Messages {
		msg := &bundle.Messages[i]
		msg.Content = redactSensitiveText(msg.Content)
		msg.Original = redactSensitiveText(msg.Original)
		msg.ReasoningContent = redactSensitiveText(msg.ReasoningContent)
		if msg.ReasoningSignature != "" {
			msg.ReasoningSignature = "[REDACTED_REASONING_SIGNATURE]"
		}
		for j, image := range msg.Images {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(image)), "data:") {
				msg.Images[j] = "[REDACTED_IMAGE_DATA_URL]"
			} else {
				msg.Images[j] = redactSensitiveText(image)
			}
		}
		for j := range msg.NativeBlocks {
			msg.NativeBlocks[j] = redactJSON(msg.NativeBlocks[j])
		}
		for j := range msg.ToolCalls {
			msg.ToolCalls[j].Arguments = string(redactJSON([]byte(msg.ToolCalls[j].Arguments)))
			msg.ToolCalls[j].Diff = redactSensitiveText(msg.ToolCalls[j].Diff)
		}
		for j := range msg.MemoryCitations {
			msg.MemoryCitations[j].Source = redactSensitiveText(msg.MemoryCitations[j].Source)
			msg.MemoryCitations[j].Note = redactSensitiveText(msg.MemoryCitations[j].Note)
		}
	}
	for i := range bundle.Evidence {
		receipt := &bundle.Evidence[i]
		receipt.Args = redactJSON(receipt.Args)
		receipt.Command = redactSensitiveText(receipt.Command)
		receipt.Step = redactSensitiveText(receipt.Step)
		receipt.DecisionSummary = redactSensitiveText(receipt.DecisionSummary)
		for j := range receipt.Paths {
			receipt.Paths[j] = redactSensitiveText(receipt.Paths[j])
		}
		for j := range receipt.Todos {
			receipt.Todos[j].Content = redactSensitiveText(receipt.Todos[j].Content)
			receipt.Todos[j].ActiveForm = redactSensitiveText(receipt.Todos[j].ActiveForm)
		}
		if receipt.TodoStep != nil {
			receipt.TodoStep.Content = redactSensitiveText(receipt.TodoStep.Content)
			receipt.TodoStep.ActiveForm = redactSensitiveText(receipt.TodoStep.ActiveForm)
		}
	}
	for i := range bundle.Skills {
		bundle.Skills[i].Description = redactSensitiveText(bundle.Skills[i].Description)
		bundle.Skills[i].Body = redactSensitiveText(bundle.Skills[i].Body)
	}
	if bundle.Dynamic != nil {
		bundle.Dynamic.Description = redactSensitiveText(bundle.Dynamic.Description)
		bundle.Dynamic.Body = redactSensitiveText(bundle.Dynamic.Body)
	}
	for i := range bundle.History {
		bundle.History[i].Text = redactSensitiveText(bundle.History[i].Text)
	}
	bundle.Review.Reviewer = redactSensitiveText(bundle.Review.Reviewer)
	bundle.Review.Reason = redactSensitiveText(bundle.Review.Reason)
	bundle.Outcome.FinalAnswer = redactSensitiveText(bundle.Outcome.FinalAnswer)
	bundle.Outcome.ConfidenceReason = redactSensitiveText(bundle.Outcome.ConfidenceReason)
}

func redactSensitiveText(value string) string {
	if value == "" {
		return ""
	}
	value = imageDataURLPattern.ReplaceAllString(value, "[REDACTED_IMAGE_DATA_URL]")
	value = bearerCredentialPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = credentialAssignmentPattern.ReplaceAllString(value, "${1}[REDACTED]")
	return commonSecretTokenPattern.ReplaceAllString(value, "[REDACTED]")
}

func redactJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(redactSensitiveText(string(raw)))
	}
	redactJSONValue("", &value)
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(redactSensitiveText(string(raw)))
	}
	return json.RawMessage(data)
}

func redactJSONValue(key string, value *any) {
	if value == nil {
		return
	}
	if isSensitiveKey(key) {
		*value = "[REDACTED]"
		return
	}
	switch typed := (*value).(type) {
	case map[string]any:
		for childKey, child := range typed {
			redactJSONValue(childKey, &child)
			typed[childKey] = child
		}
	case []any:
		for i := range typed {
			redactJSONValue("", &typed[i])
		}
	case string:
		*value = redactSensitiveText(typed)
	}
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
	switch key {
	case "apikey", "accesstoken", "authorization", "auth", "bearer", "password", "passwd", "secret", "token", "privatekey", "clientsecret":
		return true
	default:
		return false
	}
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

func captureUsedSkillNames(opts CaptureOptions) []string {
	names := append([]string(nil), opts.UsedSkillNames...)
	names = append(names, opts.SkillName)
	if opts.Dynamic != nil {
		names = append(names, opts.Dynamic.Name)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func snapshotSkillsForCapture(skills []skill.Skill, usedNames []string, policy CaptureSanitizationPolicy) []SkillSnapshot {
	if len(skills) == 0 {
		return nil
	}
	used := skillNameSet(usedNames)
	out := make([]SkillSnapshot, 0, len(skills))
	for _, sk := range skills {
		isUsed := used[strings.ToLower(strings.TrimSpace(sk.Name))]
		if !isUsed && !policy.IncludeUnrelatedSkills {
			continue
		}
		snapshot := snapshotSkill(sk)
		if !isUsed && !policy.IncludeUnrelatedSkillBody {
			snapshot.Body = ""
		}
		out = append(out, snapshot)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func snapshotDynamicSkillForCapture(sk *skill.Skill, usedNames []string, policy CaptureSanitizationPolicy) *SkillSnapshot {
	if sk == nil {
		return nil
	}
	used := skillNameSet(usedNames)
	isUsed := used[strings.ToLower(strings.TrimSpace(sk.Name))]
	if !isUsed && !policy.IncludeUnrelatedSkills {
		return nil
	}
	snapshot := snapshotSkill(*sk)
	if !isUsed && !policy.IncludeUnrelatedSkillBody {
		snapshot.Body = ""
	}
	return &snapshot
}

func skillNameSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			out[name] = true
		}
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

type retainedBundleFile struct {
	path      string
	createdAt time.Time
}

// CleanupBundles removes expired bundles and then enforces the newest-N count
// bound. Non-bundle JSON files and symlinks are left untouched.
func CleanupBundles(dir string, policy BundleRetentionPolicy) (int, error) {
	if strings.TrimSpace(dir) == "" {
		return 0, fmt.Errorf("bundle directory is required")
	}
	if policy.MaxAge < 0 || policy.MaxCount < 0 {
		return 0, fmt.Errorf("bundle retention bounds must not be negative")
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	now := policy.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	files := make([]retainedBundleFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var header struct {
			Version   int       `json:"version"`
			ID        string    `json:"id"`
			CreatedAt time.Time `json:"created_at"`
		}
		if json.Unmarshal(raw, &header) != nil || header.Version != 2 || strings.TrimSpace(header.ID) == "" {
			continue
		}
		createdAt := header.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = info.ModTime().UTC()
		}
		files = append(files, retainedBundleFile{path: path, createdAt: createdAt})
	}

	removed := 0
	kept := files[:0]
	cutoff := now.Add(-policy.MaxAge)
	for _, file := range files {
		if policy.MaxAge > 0 && file.createdAt.Before(cutoff) {
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				return removed, err
			}
			removed++
			continue
		}
		kept = append(kept, file)
	}
	if policy.MaxCount > 0 && len(kept) > policy.MaxCount {
		sort.SliceStable(kept, func(i, j int) bool {
			if kept[i].createdAt.Equal(kept[j].createdAt) {
				return kept[i].path > kept[j].path
			}
			return kept[i].createdAt.After(kept[j].createdAt)
		})
		for _, file := range kept[policy.MaxCount:] {
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

// PruneBundles is a semantic alias for callers that treat retention as an
// explicit maintenance operation.
func PruneBundles(dir string, policy BundleRetentionPolicy) (int, error) {
	return CleanupBundles(dir, policy)
}
