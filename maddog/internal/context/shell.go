package context

import (
	"fmt"
	"regexp"
	"strings"
)

type ShellOutput struct {
	Command  string
	Output   string
	MaxLines int
}

var fileLinePattern = regexp.MustCompile(`\b[\w./\\-]+\.(go|ts|tsx|js|jsx|py|rs|java|cs|cpp|c|h|hpp|md|json|yaml|yml):\d+(?::\d+)?\b`)

func SummarizeShellOutput(in ShellOutput) string {
	maxLines := in.MaxLines
	if maxLines <= 0 {
		maxLines = 24
	}
	command := strings.TrimSpace(in.Command)
	if command == "" {
		command = "shell"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[shell output summary]\ncommand: %s\n", command)
	if strings.TrimSpace(in.Output) == "" {
		b.WriteString("result: no output\n")
		return b.String()
	}
	lines := interestingShellLines(in.Output, maxLines)
	repeats := repeatedLineCounts(in.Output)
	if len(repeats) > 0 {
		b.WriteString("deduped:\n")
		for _, r := range repeats {
			fmt.Fprintf(&b, "- %s (repeated %dx)\n", r.line, r.count)
		}
	}
	if len(lines) == 0 {
		lines = firstNonEmptyLines(in.Output, maxLines)
	}
	b.WriteString("highlights:\n")
	for _, line := range lines {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func interestingShellLines(output string, limit int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		interesting := errorLinePattern.MatchString(line) ||
			strings.HasPrefix(line, "--- FAIL:") ||
			strings.HasPrefix(line, "FAIL") ||
			strings.Contains(line, "✗") ||
			strings.Contains(line, "×") ||
			fileLinePattern.MatchString(line)
		if !interesting {
			continue
		}
		line = shortenLine(line)
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return out
}

type repeatedLine struct {
	line  string
	count int
}

func repeatedLineCounts(output string) []repeatedLine {
	counts := map[string]int{}
	order := []string{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if _, ok := counts[line]; !ok {
			order = append(order, line)
		}
		counts[line]++
	}
	out := []repeatedLine{}
	for _, line := range order {
		if counts[line] > 1 {
			out = append(out, repeatedLine{line: shortenLine(line), count: counts[line]})
		}
	}
	return out
}

func firstNonEmptyLines(output string, limit int) []string {
	out := []string{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		out = append(out, shortenLine(line))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func shortenLine(line string) string {
	if len(line) <= 240 {
		return line
	}
	return snapToRuneBoundary(line, 0, 240) + "..."
}
