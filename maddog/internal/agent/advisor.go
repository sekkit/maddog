package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
)

// AdvisorStrategyPrompt is kept stable so enabling the advisor does not churn
// the provider's cacheable system prefix between ordinary turns.
const AdvisorStrategyPrompt = `Advisor strategy:
- For complex or high-risk tasks, do read-only exploration first, then consult the advisor before substantive writes.
- For long or high-risk tasks, use any remaining consultation budget before completion to review the result.
- Do not consult for simple tasks or when the user explicitly asks to skip the advisor.
- Have the executor apply the advice first; escalate only if that attempt still fails.`

const nativeAdvisorNudgePrefix = "Native advisor consultation requested."

type AdvisorDomain string

const (
	AdvisorDomainGeneral      AdvisorDomain = "general"
	AdvisorDomainArchitecture AdvisorDomain = "architecture"
	AdvisorDomainSecurity     AdvisorDomain = "security"
	AdvisorDomainPerformance  AdvisorDomain = "performance"
	AdvisorDomainDebugging    AdvisorDomain = "debugging"
	AdvisorDomainCodeQuality  AdvisorDomain = "code_quality"
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
	Domain           AdvisorDomain
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
	if a.nativeAdvisor == nil || a.advisorSuppressed {
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
		Domain:           a.inferAdvisorDomain(reason, sig),
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
	domain := normalizeAdvisorDomain(req.Domain)
	if domain == "" {
		domain = inferAdvisorDomain(req.Reason, req.Question, req.Context)
	}
	var b strings.Builder
	b.WriteString("Automatic advisor consultation requested.\n\n")
	b.WriteString("Reason: ")
	b.WriteString(req.Reason)
	b.WriteString("\nDomain: ")
	b.WriteString(string(domain))
	b.WriteString("\nFocus: ")
	b.WriteString(advisorDomainFocus(domain))
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
	task := strings.TrimSpace(a.advisorTurnInput)
	msgs := a.session.Messages
	if task == "" {
		task = latestAdvisorTask(msgs)
	}
	if task != "" {
		writeAdvisorSection(&b, "Task and constraints", compactAdvisorText(task, 1600))
	}

	var decisions []string
	if summary := strings.TrimSpace(sig.DecisionSummary); summary != "" {
		decisions = append(decisions, summary)
	}

	maxMessages := a.advisor.MaxContextMessages
	if maxMessages > 0 && len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	var recentErrors, recentResults []string
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		switch msg.Role {
		case provider.RoleAssistant:
			if len(decisions) < 3 && strings.TrimSpace(msg.Content) != "" {
				decisions = append(decisions, compactAdvisorText(msg.Content, 360))
			}
		case provider.RoleTool:
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			name := strings.TrimSpace(msg.Name)
			if name == "" {
				name = "tool"
			}
			entry := name + ": " + compactAdvisorText(msg.Content, 420)
			if advisorResultLooksLikeError(msg, sig) {
				if len(recentErrors) < 3 {
					recentErrors = append(recentErrors, entry)
				}
			} else if len(recentResults) < 3 {
				recentResults = append(recentResults, entry)
			}
		}
	}
	reverseStrings(decisions)
	reverseStrings(recentErrors)
	reverseStrings(recentResults)
	writeAdvisorList(&b, "Recent decisions", decisions)

	if sig.ConsecutiveErrors > 0 || sig.ErrorStreak > 0 || sig.GoalAcceptanceLoop > 0 || sig.HealthScore > 0 || strings.TrimSpace(sig.LastErrorTool) != "" {
		writeAdvisorSection(&b, "Failure signal", fmt.Sprintf("consecutive_errors=%d; error_streak=%d; last_error_tool=%q; health_score=%.2f; goal_acceptance_loops=%d",
			sig.ConsecutiveErrors, sig.ErrorStreak, sig.LastErrorTool, sig.HealthScore, sig.GoalAcceptanceLoop))
	}
	writeAdvisorList(&b, "Recent errors", recentErrors)
	writeAdvisorList(&b, "Recent results", recentResults)

	out := strings.TrimSpace(b.String())
	maxChars := a.advisor.MaxContextChars
	return limitAdvisorContext(out, maxChars)
}

func (a *Agent) inferAdvisorDomain(reason string, sig evidence.FailureSignal) AdvisorDomain {
	if domain := inferAdvisorDomain(a.advisorTurnInput, sig.DecisionSummary); domain != AdvisorDomainGeneral {
		return domain
	}
	return inferAdvisorDomain(reason, sig.LastErrorTool)
}

func normalizeAdvisorDomain(domain AdvisorDomain) AdvisorDomain {
	switch AdvisorDomain(strings.ToLower(strings.TrimSpace(string(domain)))) {
	case AdvisorDomainGeneral, AdvisorDomainArchitecture, AdvisorDomainSecurity,
		AdvisorDomainPerformance, AdvisorDomainDebugging, AdvisorDomainCodeQuality:
		return AdvisorDomain(strings.ToLower(strings.TrimSpace(string(domain))))
	default:
		return ""
	}
}

func inferAdvisorDomain(parts ...string) AdvisorDomain {
	text := strings.ToLower(strings.Join(parts, "\n"))
	checks := []struct {
		domain AdvisorDomain
		terms  []string
	}{
		{AdvisorDomainSecurity, []string{"security", "secure", "vulnerability", "auth", "permission", "credential", "secret", "injection", "xss", "csrf", "threat", "exploit", "安全", "漏洞", "权限", "认证", "鉴权", "密钥"}},
		{AdvisorDomainPerformance, []string{"performance", "latency", "slow", "throughput", "benchmark", "optimiz", "bottleneck", "contention", "memory leak", "性能", "延迟", "吞吐", "内存泄漏", "优化", "瓶颈"}},
		{AdvisorDomainArchitecture, []string{"architecture", "architectural", "migration", "interface", "boundary", "distributed", "schema design", "api design", "架构", "迁移", "边界", "接口设计"}},
		{AdvisorDomainCodeQuality, []string{"refactor", "maintainab", "readability", "code quality", "cleanup", "test coverage", "lint", "重构", "可维护", "代码质量", "可读性", "测试覆盖"}},
		{AdvisorDomainDebugging, []string{"debug", "bug", "error", "fail", "panic", "crash", "regression", "broken", "root cause", "flaky", "timeout", "修复", "调试", "错误", "失败", "崩溃", "回归"}},
	}
	for _, check := range checks {
		for _, term := range check.terms {
			if strings.Contains(text, term) {
				return check.domain
			}
		}
	}
	return AdvisorDomainGeneral
}

func advisorDomainFocus(domain AdvisorDomain) string {
	switch normalizeAdvisorDomain(domain) {
	case AdvisorDomainArchitecture:
		return "Check boundaries, interfaces, tradeoffs, migration risk, and reversibility."
	case AdvisorDomainSecurity:
		return "Check trust boundaries, authorization, data exposure, abuse cases, and least privilege."
	case AdvisorDomainPerformance:
		return "Require measurements; identify bottlenecks, complexity, contention, and regression risk."
	case AdvisorDomainDebugging:
		return "Prioritize reproduction evidence, root cause, the smallest corrective change, and verification."
	case AdvisorDomainCodeQuality:
		return "Review correctness, maintainability, API clarity, duplication, and focused tests."
	default:
		return "Challenge assumptions and identify the safest concrete next action."
	}
}

func advisorExplicitlySkipped(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	for _, phrase := range []string{
		"skip advisor", "skip the advisor", "without advisor", "no advisor",
		"do not use advisor", "don't use advisor", "do not consult advisor", "don't consult advisor",
		"advisor: skip", "advisor=off", "advisor off",
		"跳过advisor", "跳过 advisor", "不要使用advisor", "不要使用 advisor",
		"不要咨询advisor", "不要咨询 advisor", "无需advisor", "无需 advisor",
		"不调用advisor", "不调用 advisor", "跳过顾问", "不要咨询顾问", "不用顾问",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func latestAdvisorTask(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != provider.RoleUser || advisorGeneratedUserMessage(msgs[i].Content) {
			continue
		}
		return strings.TrimSpace(msgs[i].Content)
	}
	return ""
}

func advisorGeneratedUserMessage(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "Advisor guidance") ||
		strings.HasPrefix(trimmed, nativeAdvisorNudgePrefix) ||
		strings.HasPrefix(trimmed, MidTurnSteerPrefix) ||
		strings.HasPrefix(trimmed, "The previous assistant response") ||
		strings.HasPrefix(trimmed, "Do not call any more tools")
}

func advisorResultLooksLikeError(msg provider.Message, sig evidence.FailureSignal) bool {
	if strings.TrimSpace(sig.LastErrorTool) != "" && msg.Name == sig.LastErrorTool {
		return true
	}
	lower := strings.ToLower(msg.Content)
	for _, term := range []string{"error", "failed", "failure", "panic", "blocked", "denied", "invalid", "timeout", "错误", "失败", "拒绝", "超时"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func compactAdvisorText(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return utf8Prefix(s, max-3) + "..."
}

func writeAdvisorSection(b *strings.Builder, title, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(title)
	b.WriteString(":\n")
	b.WriteString(value)
}

func writeAdvisorList(b *strings.Builder, title string, values []string) {
	if len(values) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(title)
	b.WriteString(":\n")
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
}

func reverseStrings(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func limitAdvisorContext(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	const marker = "\n[advisor context truncated]\n"
	if max <= len(marker)+2 {
		return utf8Prefix(s, max)
	}
	available := max - len(marker)
	headSize := available * 2 / 3
	tailSize := available - headSize
	head := utf8Prefix(s, headSize)
	tail := utf8Suffix(s, tailSize)
	return head + marker + tail
}

func utf8Prefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.ValidString(s[:n]) {
		n--
	}
	return s[:n]
}

func utf8Suffix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func (a *Agent) invokeFallbackAdvisor(ctx context.Context, req AdvisorRequest) (string, error) {
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
	advice, err := a.advisorRunner(ctx, req)
	if err != nil {
		a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelWarn, Text: "advisor consultation failed: " + err.Error(),
			Advisor: event.AdvisorConsultation{Reason: req.Reason, Question: req.Question, Domain: string(req.Domain), Advice: err.Error()}})
		return "", err
	}
	advice = strings.TrimSpace(advice)
	if advice == "" {
		err = fmt.Errorf("advisor returned empty guidance")
		a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelWarn, Text: err.Error(),
			Advisor: event.AdvisorConsultation{Reason: req.Reason, Question: req.Question, Domain: string(req.Domain)}})
		return "", err
	}
	a.advisorTurnUses++
	a.advisorSessionUses++
	turnRemaining, sessionRemaining = a.advisorRemaining()
	payload := event.AdvisorConsultation{
		Reason:               req.Reason,
		Question:             req.Question,
		Advice:               advice,
		Domain:               string(req.Domain),
		UsesThisTurn:         a.advisorTurnUses,
		UsesThisSession:      a.advisorSessionUses,
		RemainingThisTurn:    turnRemaining,
		RemainingThisSession: sessionRemaining,
		MaxUsesPerTurn:       a.advisor.MaxUsesPerTurn,
		MaxUsesPerSession:    a.advisor.MaxUsesPerSession,
	}
	a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelInfo, Text: "advisor consulted: " + req.Reason, Advisor: payload})
	return advice, nil
}

func (a *Agent) recordNativeAdvisorActivity(usage *provider.Usage, nativeBlocks []json.RawMessage) {
	if a == nil {
		return
	}
	resultKeys := nativeAdvisorResultKeys(nativeBlocks)
	newResultKeys := a.markNativeAdvisorResults(resultKeys)

	var iterations []provider.UsageIteration
	if usage != nil {
		for _, iteration := range usage.Iterations {
			if isAdvisorUsageIteration(iteration) {
				iterations = append(iterations, iteration)
			}
		}
	}
	if len(iterations) > 0 {
		for i := range iterations {
			a.recordNativeAdvisorUse(&iterations[i])
		}
		return
	}
	for range newResultKeys {
		a.recordNativeAdvisorUse(nil)
	}
}

func isAdvisorUsageIteration(iteration provider.UsageIteration) bool {
	typ := strings.ToLower(strings.TrimSpace(iteration.Type))
	return typ == "advisor" || strings.Contains(typ, "advisor")
}

func nativeAdvisorResultKeys(blocks []json.RawMessage) []string {
	var keys []string
	local := make(map[string]struct{})
	for _, block := range blocks {
		var meta struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			ToolUseID string `json:"tool_use_id"`
		}
		if json.Unmarshal(block, &meta) != nil || meta.Type != "advisor_tool_result" {
			continue
		}
		key := strings.TrimSpace(meta.ToolUseID)
		if key == "" {
			key = strings.TrimSpace(meta.ID)
		}
		if key == "" {
			sum := sha256.Sum256(block)
			key = hex.EncodeToString(sum[:])
		}
		if _, ok := local[key]; ok {
			continue
		}
		local[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func (a *Agent) markNativeAdvisorResults(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	if a.nativeAdvisorResultSeen == nil {
		a.nativeAdvisorResultSeen = make(map[string]struct{})
	}
	newKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := a.nativeAdvisorResultSeen[key]; ok {
			continue
		}
		a.nativeAdvisorResultSeen[key] = struct{}{}
		newKeys = append(newKeys, key)
	}
	return newKeys
}

func (a *Agent) restoreNativeAdvisorSession(messages []provider.Message) {
	if a == nil {
		return
	}
	a.advisorSessionUses = 0
	a.nativeAdvisorResultSeen = nil
	for _, message := range messages {
		if message.Role == provider.RoleUser && strings.HasPrefix(strings.TrimSpace(message.Content), "Advisor guidance") {
			a.advisorSessionUses++
		}
		if message.Role != provider.RoleAssistant {
			continue
		}
		for range a.markNativeAdvisorResults(nativeAdvisorResultKeys(message.NativeBlocks)) {
			a.advisorSessionUses++
		}
	}
}

func (a *Agent) requestNativeAdvisorConsultation(sig evidence.FailureSignal, decision UpgradeDecision) bool {
	if a == nil || a.nativeAdvisor == nil || a.advisorSuppressed || a.nativeAdvisorNudged {
		return false
	}
	turnRemaining, sessionRemaining := a.advisorRemaining()
	if turnRemaining <= 0 || sessionRemaining == 0 {
		return false
	}
	req := a.buildAdvisorRequest(sig, decision)
	a.session.Add(provider.Message{
		Role: provider.RoleUser,
		Content: nativeAdvisorNudgePrefix + " Call the native advisor before continuing; it receives the conversation automatically. Focus: " +
			strings.TrimSpace(req.Question),
	})
	a.nativeAdvisorNudged = true
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "native advisor consultation requested before frontier escalation"})
	return true
}

func (a *Agent) recordNativeAdvisorUse(iteration *provider.UsageIteration) {
	a.advisorTurnUses++
	a.advisorSessionUses++
	turnRemaining, sessionRemaining := a.advisorRemaining()
	sig := evidence.FailureSignal{}
	if a.evidence != nil {
		sig = a.evidence.FailureSignal()
	}
	domain := a.inferAdvisorDomain("native advisor", sig)
	a.sink.Emit(event.Event{
		Kind:  event.Advisor,
		Level: event.LevelInfo,
		Text:  "native advisor consulted",
		Advisor: event.AdvisorConsultation{
			Reason:               "native advisor",
			Domain:               string(domain),
			UsesThisTurn:         a.advisorTurnUses,
			UsesThisSession:      a.advisorSessionUses,
			RemainingThisTurn:    turnRemaining,
			RemainingThisSession: sessionRemaining,
			MaxUsesPerTurn:       a.advisor.MaxUsesPerTurn,
			MaxUsesPerSession:    a.advisor.MaxUsesPerSession,
		},
	})

	advisorUsage := &provider.Usage{}
	model := ""
	if iteration != nil {
		advisorUsage.PromptTokens = iteration.InputTokens + iteration.CacheCreationInputTokens + iteration.CacheReadInputTokens
		advisorUsage.CompletionTokens = iteration.OutputTokens
		advisorUsage.TotalTokens = advisorUsage.PromptTokens + advisorUsage.CompletionTokens
		advisorUsage.CacheHitTokens = iteration.CacheReadInputTokens
		advisorUsage.CacheMissTokens = iteration.InputTokens + iteration.CacheCreationInputTokens
		model = strings.TrimSpace(iteration.Model)
	}
	if model == "" && a.nativeAdvisor != nil {
		model = strings.TrimSpace(a.nativeAdvisor.Model)
	}
	profile := &event.Profile{Role: event.UsageSourceAdvisor, Model: model}
	a.sink.Emit(event.Event{
		Kind:           event.Usage,
		Usage:          advisorUsage,
		Pricing:        a.nativeAdvisorPricing,
		Profile:        profile,
		ProviderStatus: providerStatusForProfile(profile, nil),
		Source:         event.UsageSourceAdvisor,
		UsageSource:    event.UsageSourceAdvisor,
		SessionHit:     int(a.sessCacheHit.Load()),
		SessionMiss:    int(a.sessCacheMiss.Load()),
	})
}
