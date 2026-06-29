package review

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	SeverityP1 = "P1"
	SeverityP2 = "P2"
	SeverityP3 = "P3"

	RuleSecretLike           = "secret-like-token"
	RuleUnsafeShellPipe      = "unsafe-shell-pipe"
	RuleDestructiveSQL       = "destructive-sql"
	RuleMissingErrorHandling = "missing-error-handling"
	RuleLargeDiff            = "large-diff-risk"
)

type Options struct {
	LargeDiffAddedLines int
}

type Stats struct {
	Files        int
	AddedLines   int
	DeletedLines int
	Large        bool
}

type Finding struct {
	RuleID   string
	Severity string
	File     string
	Line     int
	Message  string
	Evidence string
}

type Report struct {
	Stats    Stats
	Findings []Finding
}

var (
	secretNameRE  = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password)\s*[:=]\s*["'][A-Za-z0-9_./+=:-]{16,}["']`)
	secretValueRE = regexp.MustCompile(`(?i)((api[_-]?key|secret|token|password)\s*[:=]\s*["'])[A-Za-z0-9_./+=:-]{8,}(["'])`)
	awsKeyRE      = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
)

func AnalyzeUnifiedDiff(diff string, opts Options) Report {
	if opts.LargeDiffAddedLines <= 0 {
		opts.LargeDiffAddedLines = 400
	}
	var report Report
	currentFile := ""
	newLine := 0
	seenFiles := map[string]bool{}
	for _, raw := range strings.Split(diff, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			currentFile = parseDiffFile(line)
			if currentFile != "" && !seenFiles[currentFile] {
				seenFiles[currentFile] = true
				report.Stats.Files++
			}
			continue
		}
		if strings.HasPrefix(line, "@@") {
			newLine = parseNewHunkStart(line)
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			if strings.HasPrefix(line, "+++") {
				if file := parsePlainNewFile(line); file != "" && currentFile == "" {
					currentFile = file
					if !seenFiles[currentFile] {
						seenFiles[currentFile] = true
						report.Stats.Files++
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, `\ `) {
			continue
		}
		if strings.HasPrefix(line, "+") {
			report.Stats.AddedLines++
			added := strings.TrimPrefix(line, "+")
			report.Findings = append(report.Findings, analyzeAddedLine(currentFile, newLine, added)...)
			newLine++
			continue
		}
		if strings.HasPrefix(line, "-") {
			report.Stats.DeletedLines++
			continue
		}
		if currentFile != "" && newLine > 0 {
			newLine++
		}
	}
	if report.Stats.AddedLines > opts.LargeDiffAddedLines {
		report.Stats.Large = true
		report.Findings = append(report.Findings, Finding{
			RuleID:   RuleLargeDiff,
			Severity: SeverityP3,
			Message:  "large diff; prioritize file summaries and code intelligence context",
			Evidence: report.Stats.StatsSummary(),
		})
	}
	sort.SliceStable(report.Findings, func(i, j int) bool {
		if severityRank(report.Findings[i].Severity) != severityRank(report.Findings[j].Severity) {
			return severityRank(report.Findings[i].Severity) < severityRank(report.Findings[j].Severity)
		}
		if report.Findings[i].File != report.Findings[j].File {
			return report.Findings[i].File < report.Findings[j].File
		}
		return report.Findings[i].Line < report.Findings[j].Line
	})
	return report
}

func analyzeAddedLine(file string, line int, added string) []Finding {
	trimmed := strings.TrimSpace(added)
	var out []Finding
	if secretNameRE.MatchString(trimmed) || awsKeyRE.MatchString(trimmed) {
		out = append(out, Finding{RuleID: RuleSecretLike, Severity: SeverityP1, File: file, Line: line, Message: "secret-like token added", Evidence: redact(trimmed)})
	}
	if isDocsPath(file) || isCommentOrExample(trimmed) {
		return out
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "curl ") && (strings.Contains(lower, "| sh") || strings.Contains(lower, "| bash")) {
		out = append(out, Finding{RuleID: RuleUnsafeShellPipe, Severity: SeverityP1, File: file, Line: line, Message: "remote shell installer executes without verification", Evidence: trimmed})
	}
	if (strings.Contains(lower, "invoke-webrequest") || strings.Contains(lower, "iwr ")) && strings.Contains(lower, "| iex") {
		out = append(out, Finding{RuleID: RuleUnsafeShellPipe, Severity: SeverityP1, File: file, Line: line, Message: "remote PowerShell executes without verification", Evidence: trimmed})
	}
	if strings.Contains(lower, "drop table") || strings.Contains(lower, "truncate table") || (strings.Contains(lower, "delete from") && !strings.Contains(lower, " where ")) {
		out = append(out, Finding{RuleID: RuleDestructiveSQL, Severity: SeverityP1, File: file, Line: line, Message: "destructive SQL added", Evidence: trimmed})
	}
	if strings.Contains(trimmed, ", _ :=") || strings.Contains(trimmed, ", _ =") || strings.Contains(trimmed, "_ = os.") {
		out = append(out, Finding{RuleID: RuleMissingErrorHandling, Severity: SeverityP2, File: file, Line: line, Message: "new code appears to ignore an error", Evidence: trimmed})
	}
	return out
}

func parseDiffFile(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return ""
	}
	path := strings.TrimPrefix(parts[3], "b/")
	if path == "/dev/null" {
		path = strings.TrimPrefix(parts[2], "a/")
	}
	return path
}

func parsePlainNewFile(line string) string {
	path := strings.TrimSpace(strings.TrimPrefix(line, "+++"))
	path = strings.TrimPrefix(path, "b/")
	path = strings.TrimPrefix(path, "new/")
	if path == "/dev/null" || path == "" {
		return ""
	}
	return path
}

func parseNewHunkStart(line string) int {
	idx := strings.Index(line, " +")
	if idx < 0 {
		return 0
	}
	rest := strings.TrimPrefix(line[idx+1:], "+")
	end := strings.IndexAny(rest, " ,")
	if end >= 0 {
		rest = rest[:end]
	}
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func (s Stats) StatsSummary() string {
	return strings.TrimSpace(strings.Join([]string{
		intLabel(s.Files, "files"),
		intLabel(s.AddedLines, "added"),
		intLabel(s.DeletedLines, "deleted"),
	}, " · "))
}

func intLabel(n int, label string) string {
	return strconv.Itoa(n) + " " + label
}

func severityRank(severity string) int {
	switch severity {
	case SeverityP1:
		return 1
	case SeverityP2:
		return 2
	default:
		return 3
	}
}

func redact(s string) string {
	s = RedactSecrets(s)
	if len(s) > 96 {
		return s[:64] + "...[redacted]"
	}
	return s
}

func RedactSecrets(s string) string {
	s = secretValueRE.ReplaceAllString(s, "${1}[redacted]${3}")
	s = awsKeyRE.ReplaceAllString(s, "AKIA[redacted]")
	return s
}

func RedactEvidence(s string) string {
	return redact(s)
}

func isDocsPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx") || strings.Contains(path, "/docs/") || strings.HasPrefix(path, "docs/")
}

func isCommentOrExample(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "//") || strings.HasPrefix(lower, "#") || strings.HasPrefix(lower, "--") || strings.HasPrefix(lower, "example:")
}
