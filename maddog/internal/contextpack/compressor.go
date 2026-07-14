package contextpack

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"maddog/internal/contextpackroute"
)

const (
	defaultThresholdBytes = 8 * 1024
	defaultMaxBytes       = 4 * 1024
)

var pathLinePattern = regexp.MustCompile(`(^|[\s(])(([A-Za-z]:[\\/])?[A-Za-z0-9_./\\-]+:\d+(:\d+)?)`)

// ToolOutput is the raw text produced by a tool before it is added to model
// context.
type ToolOutput struct {
	ToolName string
	// Shell and GOOS describe the execution environment, not the model-visible
	// tool name. Empty values require conservative command classification.
	Shell    string
	GOOS     string
	Args     string
	Output   string
	Error    string
	ReadOnly bool
}

// Options controls deterministic tool-output compression.
type Options struct {
	Policy         string
	ThresholdBytes int
	MaxBytes       int
	RawRef         string
}

// ToolOutputCompressor is the integration point used by the agent loop. It is
// deterministic: implementations must not call a model or depend on wall time.
type ToolOutputCompressor interface {
	Compress(ToolOutput, Options) Result
}

// DefaultCompressor is Maddog's built-in deterministic compressor.
type DefaultCompressor struct{}

// Route selects the model-visible ContextPack transform.
type Route = contextpackroute.Route

const (
	RoutePassthrough = contextpackroute.RoutePassthrough
	RouteGeneric     = contextpackroute.RouteGeneric
	RouteProfile     = contextpackroute.RouteProfile
)

type ParseQuality string

const (
	ParseQualityExact       ParseQuality = "exact"
	ParseQualityDegraded    ParseQuality = "degraded"
	ParseQualityPassthrough ParseQuality = "passthrough"
)

// Result is the model-visible output plus compression metadata.
type Result struct {
	Content          string
	RawRef           string
	Route            Route
	Profile          string
	Quality          ParseQuality
	QualityReason    string
	UnparsedLines    int
	UnparsedSamples  []string
	OmittedLines     int
	Strategy         string
	Summary          string
	RawChars         int
	CompressedChars  int
	SavedChars       int
	RawTokens        int
	CompressedTokens int
	SavedTokens      int
}

func passthroughResult(content, reason string) Result {
	return Result{
		Content:       content,
		Route:         RoutePassthrough,
		Quality:       ParseQualityPassthrough,
		QualityReason: reason,
	}
}

type lineRun struct {
	text  string
	count int
}

// Compress reduces high-volume tool output while keeping failure-oriented
// context intact. Passthrough results omit savings and raw-reference metadata.
func Compress(output ToolOutput, opts Options) Result {
	return DefaultCompressor{}.Compress(output, opts)
}

// Compress reduces high-volume tool output while keeping failure-oriented
// context intact. Passthrough results omit savings and raw-reference metadata.
func (DefaultCompressor) Compress(output ToolOutput, opts Options) Result {
	raw := output.Output
	if output.Error != "" {
		raw = appendError(raw, output.Error)
	}
	policy := normalizePolicy(opts.Policy)
	if policy == "off" {
		return passthroughResult(raw, "policy_off")
	}
	if policy != "aggressive" && len(raw) <= effectiveThreshold(opts.ThresholdBytes) {
		return passthroughResult(raw, "below_threshold")
	}

	maxBytes := effectiveMax(opts.MaxBytes)
	var strategy string
	route := RouteGeneric
	profile := "generic"
	quality := ParseQualityDegraded
	qualityReason := "heuristic signal extraction and sampling"
	var unparsedLines int
	var unparsedSamples []string
	content := ""
	if shellResult, ok := compressShellOutput(output, raw, maxBytes); ok {
		content = shellResult.content
		strategy = shellResult.strategy
		route = shellResult.route
		profile = shellResult.profile
		quality = shellResult.quality
		qualityReason = shellResult.qualityReason
		unparsedLines = shellResult.unparsedLines
		unparsedSamples = append([]string(nil), shellResult.unparsedSamples...)
	} else {
		content = compressText(output, raw, maxBytes)
		unparsedLines, unparsedSamples = omittedRawLineDetails(raw, content)
	}
	if content == "" {
		content = headTail(raw, maxBytes)
	}
	if maxBytes > 0 && len(content) > maxBytes {
		content = trimLines(content, maxBytes)
	}
	rawChars := runeCount(raw)
	compressedChars := runeCount(content)
	if compressedChars >= rawChars {
		content = headTail(raw, maxBytes)
		compressedChars = runeCount(content)
		route = RouteGeneric
		profile = "generic"
		quality = ParseQualityDegraded
		qualityReason = "bounded raw head/tail fallback"
		unparsedLines, unparsedSamples = omittedRawLineDetails(raw, content)
	}
	if compressedChars >= rawChars {
		return passthroughResult(raw, "no_savings")
	}

	result := Result{
		Content:         content,
		RawRef:          opts.RawRef,
		Route:           route,
		Profile:         profile,
		Quality:         quality,
		QualityReason:   qualityReason,
		UnparsedLines:   unparsedLines,
		UnparsedSamples: append([]string(nil), unparsedSamples...),
		OmittedLines:    omittedLineCount(raw, content),
		Strategy:        strategy,
		Summary:         summaryFor(output, raw, content),
		RawChars:        rawChars,
		CompressedChars: compressedChars,
	}
	if result.Strategy == "" {
		result.Strategy = strategyFor(output, raw)
	}
	result.SavedChars = result.RawChars - result.CompressedChars
	if result.SavedChars < 0 {
		result.SavedChars = 0
	}
	result.RawTokens = estimateTokens(raw)
	result.CompressedTokens = estimateTokens(content)
	result.SavedTokens = result.RawTokens - result.CompressedTokens
	if result.SavedTokens < 0 {
		result.SavedTokens = 0
	}
	return result
}

func omittedLineCount(raw, visible string) int {
	omitted, _ := omittedRawLineDetails(raw, visible)
	return omitted
}

func omittedRawLineDetails(raw, visible string) (int, []string) {
	visibleCounts := make(map[string]int)
	for _, line := range outputLines(visible) {
		visibleCounts[line]++
	}
	omitted := 0
	samples := make([]string, 0, 3)
	for _, line := range outputLines(raw) {
		if visibleCounts[line] > 0 {
			visibleCounts[line]--
			continue
		}
		omitted++
		if strings.TrimSpace(line) != "" && len(samples) < 3 {
			samples = append(samples, line)
		}
	}
	return omitted, samples
}

func normalizePolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "auto":
		return "auto"
	case "off", "aggressive":
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return "auto"
	}
}

func appendError(raw, errText string) string {
	if raw == "" {
		return errText
	}
	if strings.HasSuffix(raw, "\n") {
		return raw + errText
	}
	return raw + "\n" + errText
}

func effectiveThreshold(threshold int) int {
	if threshold > 0 {
		return threshold
	}
	return defaultThresholdBytes
}

func effectiveMax(maxBytes int) int {
	if maxBytes > 0 {
		return maxBytes
	}
	return defaultMaxBytes
}

func compressText(output ToolOutput, raw string, maxBytes int) string {
	runs := runLengthLines(raw)
	if len(runs) == 0 {
		return ""
	}

	var signalLines []string
	var repeatedLines []string
	for _, run := range runs {
		line := formatRun(run)
		if isSignalLine(run.text) {
			signalLines = appendUnique(signalLines, line)
		}
		if run.count > 1 {
			repeatedLines = appendUnique(repeatedLines, line)
		}
	}

	var lines []string
	lines = append(lines, signalLines...)
	lines = append(lines, repeatedLines...)

	if len(signalLines) == 0 {
		lines = append(lines, sampleRuns(runs, 4)...)
	}
	if output.ToolName != "" {
		lines = append(lines, "[compressed "+output.ToolName+" output]")
	} else {
		lines = append(lines, "[compressed tool output]")
	}
	return joinPriorityLines(lines, maxBytes)
}

func strategyFor(output ToolOutput, raw string) string {
	if output.ToolName == "bash" || strings.Contains(strings.ToLower(output.Args), "test") {
		return "shell-error-first"
	}
	for _, run := range runLengthLines(raw) {
		if run.count > 1 {
			return "log-dedupe"
		}
	}
	return "head-tail"
}

func summaryFor(output ToolOutput, raw, content string) string {
	runs := runLengthLines(raw)
	repeated := 0
	signals := 0
	for _, run := range runs {
		if run.count > 1 {
			repeated++
		}
		if isSignalLine(run.text) {
			signals++
		}
	}
	toolName := strings.TrimSpace(output.ToolName)
	if toolName == "" {
		toolName = "tool"
	}
	parts := []string{
		toolName + " output compressed",
		decimal(runeCount(raw)) + " raw chars",
		decimal(runeCount(content)) + " visible chars",
	}
	if signals > 0 {
		parts = append(parts, decimal(signals)+" signal lines")
	}
	if repeated > 0 {
		parts = append(parts, decimal(repeated)+" repeated runs")
	}
	return strings.Join(parts, "; ")
}

func runLengthLines(raw string) []lineRun {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(normalized, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}

	runs := make([]lineRun, 0, len(parts))
	for _, line := range parts {
		if len(runs) > 0 && runs[len(runs)-1].text == line {
			runs[len(runs)-1].count++
			continue
		}
		runs = append(runs, lineRun{text: line, count: 1})
	}
	return runs
}

func formatRun(run lineRun) string {
	if run.count <= 1 {
		return run.text
	}
	return run.text + " (repeated " + decimal(run.count) + " times)"
}

func isSignalLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)
	if pathLinePattern.MatchString(trimmed) {
		return true
	}

	signalNeedles := []string{
		"--- fail:",
		"fail",
		"failed",
		"panic:",
		"fatal",
		"error",
		"exception",
		"traceback",
		"undefined:",
		"expected",
		"actual",
		"assert",
		"exit status",
		"exit code",
		"no such file",
		"not found",
		"permission denied",
		"timeout",
	}
	for _, needle := range signalNeedles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func appendUnique(lines []string, line string) []string {
	for _, existing := range lines {
		if existing == line {
			return lines
		}
	}
	return append(lines, line)
}

func sampleRuns(runs []lineRun, keep int) []string {
	if keep <= 0 || len(runs) == 0 {
		return nil
	}
	if len(runs) <= keep*2 {
		lines := make([]string, 0, len(runs))
		for _, run := range runs {
			lines = append(lines, formatRun(run))
		}
		return lines
	}

	lines := make([]string, 0, keep*2+1)
	for _, run := range runs[:keep] {
		lines = append(lines, formatRun(run))
	}
	lines = append(lines, "[snip]")
	for _, run := range runs[len(runs)-keep:] {
		lines = append(lines, formatRun(run))
	}
	return lines
}

func joinPriorityLines(lines []string, maxBytes int) string {
	if len(lines) == 0 {
		return ""
	}
	if maxBytes <= 0 {
		return strings.Join(lines, "\n")
	}

	var b strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		need := len(line)
		if b.Len() > 0 {
			need++
		}
		if b.Len()+need <= maxBytes {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
			continue
		}
		if b.Len() == 0 {
			return trimString(line, maxBytes)
		}
	}
	return b.String()
}

func headTail(raw string, maxBytes int) string {
	if maxBytes <= 0 || len(raw) <= maxBytes {
		return raw
	}
	const marker = "\n[snip]\n"
	if maxBytes <= len(marker)+2 {
		return trimString(raw, maxBytes)
	}

	keep := (maxBytes - len(marker)) / 2
	head := trimString(raw, keep)
	tailLen := maxBytes - len(marker) - len(head)
	if tailLen <= 0 {
		return head
	}
	return head + marker + tailString(raw, tailLen)
}

func trimLines(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	return joinPriorityLines(strings.Split(content, "\n"), maxBytes)
}

func trimString(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	end := 0
	for end < len(s) {
		_, size := utf8.DecodeRuneInString(s[end:])
		if end+size > maxBytes {
			break
		}
		end += size
	}
	return s[:end]
}

func tailString(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	start := len(s)
	kept := 0
	for start > 0 {
		_, size := utf8.DecodeLastRuneInString(s[:start])
		if kept+size > maxBytes {
			break
		}
		start -= size
		kept += size
	}
	return s[start:]
}

func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}

func decimal(n int) string {
	if n == 0 {
		return "0"
	}

	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	ascii := 0
	cjk := 0
	other := 0
	for _, r := range s {
		switch {
		case r <= 0x7f:
			ascii++
		case isCJKRune(r):
			cjk++
		default:
			other++
		}
	}
	tokens := (ascii + 3) / 4
	tokens += cjk
	tokens += (other + 1) / 2
	if tokens == 0 {
		return 1
	}
	return tokens
}

func isCJKRune(r rune) bool {
	return (r >= 0x4e00 && r <= 0x9fff) ||
		(r >= 0x3400 && r <= 0x4dbf) ||
		(r >= 0x3040 && r <= 0x30ff) ||
		(r >= 0xac00 && r <= 0xd7af)
}
