package contextpack

import (
	"encoding/json"
	"strings"
)

type shellCompression struct {
	content         string
	route           Route
	profile         string
	quality         ParseQuality
	qualityReason   string
	unparsedLines   int
	unparsedSamples []string
	strategy        string
}

func compressShellOutput(output ToolOutput, raw string, maxBytes int) (shellCompression, bool) {
	semantics := commandSemanticsFor(output)
	if !semantics.supported() {
		return shellCompression{}, false
	}
	cmd := commandText(output.Args)
	desc := describeCommand(semantics, cmd)
	if desc.Executable != "" {
		for _, profile := range builtinShellProfiles {
			if !profile.match(desc) {
				continue
			}
			parsed := profile.parse(raw, maxBytes)
			if parsed.quality == ParseQualityPassthrough || parsed.content == "" {
				content := compressText(output, raw, maxBytes)
				lines, samples := omittedRawLineDetails(raw, content)
				return shellCompression{
					content:         content,
					route:           RouteGeneric,
					profile:         "generic",
					quality:         ParseQualityDegraded,
					qualityReason:   profile.id + " profile passthrough: " + parsed.qualityReason,
					unparsedLines:   lines,
					unparsedSamples: samples,
				}, true
			}
			return shellCompression{
				content:         parsed.content,
				route:           RouteProfile,
				profile:         profile.id,
				quality:         parsed.quality,
				qualityReason:   parsed.qualityReason,
				unparsedLines:   parsed.unparsedLines,
				unparsedSamples: parsed.unparsedSamples,
				strategy:        profile.strategy,
			}, true
		}
	}
	if hasRepeatedNonAdjacentLines(raw) {
		if content := compressRepeatedLogOutput(raw, maxBytes); content != "" {
			lines, samples := omittedRawLineDetails(raw, content)
			return shellCompression{
				content:         content,
				route:           RouteGeneric,
				profile:         "generic",
				quality:         ParseQualityDegraded,
				qualityReason:   "heuristic repeated-log extraction and sampling",
				unparsedLines:   lines,
				unparsedSamples: samples,
				strategy:        "server-log-dedupe",
			}, true
		}
	}
	return shellCompression{}, false
}

func commandSemanticsFor(output ToolOutput) commandSemantics {
	shell := strings.TrimSpace(output.Shell)
	if shell == "" {
		shell = output.ToolName
	}
	return newCommandSemantics(shell, output.GOOS)
}

func commandText(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err == nil {
		for _, key := range []string{"command", "cmd", "script"} {
			if value, ok := parsed[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return strings.TrimSpace(args)
}

type profileAssessment struct {
	recognized int
	unparsed   int
	samples    []string
	signals    []string
}

func parseRipgrepProfileOutput(raw string, maxBytes int) profileOutput {
	return parseHeuristicProfile(raw, maxBytes, compressRipgrepOutput, func(line string) bool {
		_, ok := parseRipgrepLine(line)
		return ok
	})
}

func parseGitStatusProfileOutput(raw string, maxBytes int) profileOutput {
	return parseHeuristicProfile(raw, maxBytes, compressGitStatusOutput, isGitStatusShortLine)
}

func parseGitDiffProfileOutput(raw string, maxBytes int) profileOutput {
	return parseHeuristicProfile(raw, maxBytes, compressGitDiffOutput, isGitDiffPatchLine)
}

func parseGoTestProfileOutput(raw string, maxBytes int) profileOutput {
	return parseHeuristicProfile(raw, maxBytes, compressGoTestOutput, isGoTestFailureLine)
}

func parseNPMBuildProfileOutput(raw string, maxBytes int) profileOutput {
	return parseHeuristicProfile(raw, maxBytes, compressNPMBuildOutput, isNPMBuildFailureLine)
}

func parseNPMTestProfileOutput(raw string, maxBytes int) profileOutput {
	return parseHeuristicProfile(raw, maxBytes, compressNPMTestOutput, isNPMTestFailureLine)
}

func parseHeuristicProfile(raw string, maxBytes int, compress func(string, int) string, recognizes func(string) bool) profileOutput {
	assessment := assessProfileOutput(raw, recognizes)
	if assessment.recognized == 0 {
		return profileOutput{quality: ParseQualityPassthrough, qualityReason: "output shape not recognized"}
	}
	content := compress(raw, maxBytes)
	if content == "" {
		return profileOutput{quality: ParseQualityPassthrough, qualityReason: "profile produced no useful output"}
	}
	if len(assessment.signals) > 0 {
		lines := append([]string(nil), assessment.signals...)
		for _, line := range strings.Split(content, "\n") {
			lines = appendUnique(lines, line)
		}
		content = joinPriorityLines(lines, maxBytes)
	}
	reason := "heuristic text profile"
	if assessment.unparsed > 0 {
		reason += " with unparsed lines"
	}
	return profileOutput{
		content:         content,
		quality:         ParseQualityDegraded,
		qualityReason:   reason,
		unparsedLines:   assessment.unparsed,
		unparsedSamples: assessment.samples,
	}
}

func assessProfileOutput(raw string, recognizes func(string) bool) profileAssessment {
	var out profileAssessment
	for _, line := range outputLines(raw) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if recognizes(line) {
			out.recognized++
			continue
		}
		out.unparsed++
		if len(out.samples) < 3 {
			out.samples = appendUnique(out.samples, line)
		}
		if isSignalLine(line) && len(out.signals) < 3 {
			out.signals = appendUnique(out.signals, line)
		}
	}
	return out
}

func isGitStatusShortLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "##") || strings.HasPrefix(trimmed, "??") || strings.HasPrefix(trimmed, "!!") {
		return true
	}
	if len(line) < 3 || line[2] != ' ' {
		return false
	}
	return isGitStatusCode(line[0]) && isGitStatusCode(line[1])
}

func isGitStatusCode(ch byte) bool {
	return ch == ' ' || strings.ContainsRune("MADRCU?!", rune(ch))
}

func isGitDiffPatchLine(line string) bool {
	return strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "index ") ||
		strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
		strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+") ||
		strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") ||
		strings.HasPrefix(line, "\\ No newline at end of file")
}

func isGoTestFailureLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "panic:") ||
		strings.HasPrefix(trimmed, "FAIL") || strings.HasPrefix(trimmed, "exit status") ||
		strings.Contains(lower, "expected") || strings.Contains(lower, "actual") ||
		(pathLinePattern.MatchString(trimmed) && isSignalLine(trimmed))
}

func isNPMBuildFailureLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.Contains(lower, " error ") || strings.Contains(lower, " - error ") ||
		strings.Contains(lower, "npm err!") || strings.Contains(lower, "elifecycle") ||
		strings.Contains(lower, "exit code") || strings.Contains(lower, "command failed")
}

func isNPMTestFailureLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "FAIL") || strings.Contains(lower, " failed") ||
		strings.Contains(lower, "expected") || strings.Contains(lower, "received") ||
		strings.Contains(lower, "actual") || strings.Contains(lower, " at ") ||
		strings.Contains(lower, "test suites:") || strings.Contains(lower, "tests:") ||
		pathLinePattern.MatchString(trimmed)
}

func compressGoTestOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	out := make([]string, 0, 16)
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(trimmed, "--- FAIL:"):
			out = appendUnique(out, lines[i])
			for j := i + 1; j < len(lines) && j <= i+8; j++ {
				next := strings.TrimSpace(lines[j])
				if next == "" || strings.HasPrefix(next, "===") || strings.HasPrefix(next, "--- PASS:") || strings.HasPrefix(next, "--- FAIL:") {
					break
				}
				if strings.HasPrefix(lines[j], " ") || strings.HasPrefix(lines[j], "\t") || isSignalLine(lines[j]) {
					out = appendUnique(out, lines[j])
				}
			}
		case strings.HasPrefix(trimmed, "panic:"),
			strings.HasPrefix(trimmed, "exit status"),
			strings.HasPrefix(trimmed, "FAIL"),
			strings.Contains(lower, "expected"),
			strings.Contains(lower, "actual"):
			out = appendUnique(out, lines[i])
		case pathLinePattern.MatchString(trimmed) && isSignalLine(trimmed):
			out = appendUnique(out, lines[i])
		}
	}
	for _, run := range runLengthLines(raw) {
		if run.count > 1 {
			out = appendUnique(out, formatRun(run))
		}
	}
	out = appendTailLines(out, lines, 3)
	return joinPriorityLines(withMarker(out, "[go test failure summary]"), maxBytes)
}

func compressNPMBuildOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	out := make([]string, 0, 12)
	capturedFatalContext := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		lower := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		if strings.Contains(lower, " error ") || strings.Contains(lower, " - error ") || strings.Contains(lower, "npm err!") ||
			strings.Contains(lower, "elifecycle") || strings.Contains(lower, "exit code") || strings.Contains(lower, "command failed") {
			out = appendUnique(out, lines[i])
			if !capturedFatalContext && strings.Contains(lower, "error") {
				capturedFatalContext = true
				for j := i + 1; j < len(lines) && j <= i+3; j++ {
					if strings.TrimSpace(lines[j]) == "" {
						break
					}
					out = appendUnique(out, lines[j])
				}
			}
		}
	}
	out = appendTailLines(out, lines, 3)
	return joinPriorityLines(withMarker(out, "[npm build error summary]"), maxBytes)
}

func compressNPMTestOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	out := make([]string, 0, 14)
	captureNext := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "FAIL") || strings.Contains(lower, " failed") ||
			strings.Contains(lower, "expected") || strings.Contains(lower, "received") ||
			strings.Contains(lower, "actual") || strings.Contains(lower, " at ") ||
			strings.Contains(lower, "test suites:") || strings.Contains(lower, "tests:") ||
			pathLinePattern.MatchString(trimmed) {
			out = appendUnique(out, line)
			if strings.HasPrefix(trimmed, "FAIL") {
				captureNext = 5
			}
			continue
		}
		if captureNext > 0 {
			out = appendUnique(out, line)
			captureNext--
		}
	}
	out = appendTailLines(out, lines, 3)
	return joinPriorityLines(withMarker(out, "[npm test failure summary]"), maxBytes)
}

func compressRepeatedLogOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	counts := map[string]int{}
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if counts[line] == 0 {
			order = append(order, line)
		}
		counts[line]++
	}
	out := make([]string, 0, 12)
	for _, line := range lines {
		if isSignalLine(line) {
			out = appendUnique(out, line)
		}
	}
	for _, line := range order {
		if counts[line] > 1 {
			out = append(out, line+" (repeated "+decimal(counts[line])+" times)")
		}
	}
	out = appendTailLines(out, lines, 3)
	out = append(out, "[log dedupe summary]")
	return joinPriorityLines(out, maxBytes)
}

type rgFileGroup struct {
	path    string
	count   int
	samples []string
}

func compressRipgrepOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	groups := map[string]*rgFileGroup{}
	order := make([]string, 0)
	for _, line := range lines {
		match, ok := parseRipgrepLine(line)
		if !ok {
			continue
		}
		path := match.path
		group := groups[path]
		if group == nil {
			group = &rgFileGroup{path: path}
			groups[path] = group
			order = append(order, path)
		}
		group.count++
		if len(group.samples) < 2 {
			group.samples = append(group.samples, line)
		}
	}
	if len(order) == 0 {
		return ""
	}
	out := make([]string, 0, len(order)*3)
	for _, path := range order {
		group := groups[path]
		out = append(out, group.path+" ("+decimal(group.count)+" matches)")
		out = append(out, group.samples...)
	}
	out = append(out, "[rg match summary]")
	return joinPriorityLines(out, maxBytes)
}

type rgMatchLine struct {
	path string
}

func parseRipgrepLine(line string) (rgMatchLine, bool) {
	parts := strings.Split(line, ":")
	for i := 1; i < len(parts)-1; i++ {
		if isDecimalField(parts[i]) {
			path := strings.Join(parts[:i], ":")
			if strings.TrimSpace(path) == "" {
				return rgMatchLine{}, false
			}
			return rgMatchLine{path: path}, true
		}
	}
	return rgMatchLine{}, false
}

func isDecimalField(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compressGitStatusOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	if len(lines) == 0 {
		return ""
	}
	modified, untracked, added, deleted, renamed := 0, 0, 0, 0, 0
	out := make([]string, 0, 12)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "##"):
			out = appendUnique(out, line)
		case strings.HasPrefix(trimmed, "??"):
			untracked++
		default:
			status := statusCode(line)
			if strings.Contains(status, "R") {
				renamed++
			}
			if strings.Contains(status, "D") {
				deleted++
			}
			if strings.Contains(status, "A") {
				added++
			}
			if strings.ContainsAny(status, "MRCU") || strings.Contains(status, "A") || strings.Contains(status, "D") {
				modified++
			}
		}
	}
	for _, summary := range []struct {
		label string
		count int
	}{
		{"modified", modified},
		{"added", added},
		{"deleted", deleted},
		{"renamed", renamed},
		{"untracked", untracked},
	} {
		if summary.count > 0 {
			out = append(out, summary.label+": "+decimal(summary.count))
		}
	}
	out = append(out, sampleStatusLines(lines, 4)...)
	out = appendTailLines(out, lines, 3)
	out = append(out, "[git status summary]")
	return joinPriorityLines(out, maxBytes)
}

func statusCode(line string) string {
	if len(line) >= 2 {
		return line[:2]
	}
	return strings.TrimSpace(line)
}

func sampleStatusLines(lines []string, keep int) []string {
	out := make([]string, 0, keep*2)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "##") {
			continue
		}
		out = append(out, line)
		if len(out) >= keep {
			break
		}
	}
	for i := len(lines) - 1; i >= 0 && len(out) < keep*2; i-- {
		line := lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "##") {
			continue
		}
		out = appendUnique(out, line)
	}
	return out
}

func compressGitDiffOutput(raw string, maxBytes int) string {
	lines := outputLines(raw)
	fileCount := 0
	out := make([]string, 0, 16)
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			fileCount++
			if fileCount <= 6 {
				out = append(out, diffPath(line))
			}
			continue
		}
		if strings.HasPrefix(line, "@@") {
			out = appendUnique(out, line)
		}
	}
	if fileCount == 0 {
		return ""
	}
	fileWord := "files"
	if fileCount == 1 {
		fileWord = "file"
	}
	out = append([]string{decimal(fileCount) + " " + fileWord + " changed"}, out...)
	out = appendTailLines(out, lines, 3)
	out = append(out, "[git diff summary]")
	return joinPriorityLines(out, maxBytes)
}

func diffPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) >= 4 {
		path := strings.TrimPrefix(parts[3], "b/")
		if path != "" {
			return path
		}
	}
	return line
}

func outputLines(raw string) []string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(normalized, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func withMarker(lines []string, marker string) []string {
	if len(lines) == 0 {
		return nil
	}
	return append(lines, marker)
}

func appendTailLines(lines, source []string, keep int) []string {
	if keep <= 0 || len(source) == 0 {
		return lines
	}
	start := len(source) - keep
	if start < 0 {
		start = 0
	}
	for _, line := range source[start:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = appendUnique(lines, line)
	}
	return lines
}

func hasRepeatedNonAdjacentLines(raw string) bool {
	lines := outputLines(raw)
	counts := map[string]int{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		counts[line]++
		if counts[line] >= 4 {
			return true
		}
	}
	return false
}
