package contextpack

import (
	"encoding/json"
	"strings"
)

type shellCompression struct {
	content  string
	strategy string
}

func compressShellOutput(output ToolOutput, raw string, maxBytes int) (shellCompression, bool) {
	if !looksLikeShell(output) {
		return shellCompression{}, false
	}
	cmd := commandText(output.Args)
	switch {
	case isRipgrepCommand(cmd):
		if content := compressRipgrepOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "rg-file-sampling"}, true
		}
	case isGitStatusCommand(cmd):
		if content := compressGitStatusOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "git-status-summary"}, true
		}
	case isGitDiffCommand(cmd):
		if content := compressGitDiffOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "git-diff-summary"}, true
		}
	case isGoTestCommand(cmd):
		if content := compressGoTestOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "go-test-failure"}, true
		}
	case isNPMTestCommand(cmd):
		if content := compressNPMTestOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "npm-test-failure"}, true
		}
	case isNPMBuildCommand(cmd):
		if content := compressNPMBuildOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "npm-build-error"}, true
		}
	}
	if hasRepeatedNonAdjacentLines(raw) {
		if content := compressRepeatedLogOutput(raw, maxBytes); content != "" {
			return shellCompression{content: content, strategy: "server-log-dedupe"}, true
		}
	}
	return shellCompression{}, false
}

func looksLikeShell(output ToolOutput) bool {
	name := strings.ToLower(strings.TrimSpace(output.ToolName))
	return name == "bash" || name == "shell" || name == "powershell" || name == "pwsh"
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
				return strings.ToLower(strings.TrimSpace(value))
			}
		}
	}
	return strings.ToLower(args)
}

func isGoTestCommand(cmd string) bool {
	return strings.Contains(cmd, "go test")
}

func isNPMBuildCommand(cmd string) bool {
	return strings.Contains(cmd, "npm run build") ||
		strings.Contains(cmd, "npm build") ||
		strings.Contains(cmd, "pnpm build") ||
		strings.Contains(cmd, "yarn build")
}

func isNPMTestCommand(cmd string) bool {
	return strings.Contains(cmd, "npm test") ||
		strings.Contains(cmd, "npm run test") ||
		strings.Contains(cmd, "pnpm test") ||
		strings.Contains(cmd, "pnpm run test") ||
		strings.Contains(cmd, "yarn test")
}

func isRipgrepCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	return cmd == "rg" || strings.HasPrefix(cmd, "rg ") || strings.Contains(cmd, " rg ")
}

func isGitStatusCommand(cmd string) bool {
	return strings.Contains(cmd, "git status")
}

func isGitDiffCommand(cmd string) bool {
	return strings.Contains(cmd, "git diff")
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
