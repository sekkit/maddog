package review

import (
	"regexp"
	"strconv"
	"strings"
)

type Options struct {
	CodeBackendAvailable   bool
	LargeDiffLineThreshold int
}

type Finding struct {
	RuleID   string `json:"ruleId"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
	Snippet  string `json:"snippet,omitempty"`
}

type Report struct {
	Findings    []Finding `json:"findings"`
	Summary     string    `json:"summary"`
	Fallback    string    `json:"fallback,omitempty"`
	DiffLines   int       `json:"diffLines"`
	LargeDiff   bool      `json:"largeDiff,omitempty"`
	CodeBackend string    `json:"codeBackend,omitempty"`
}

func AnalyzeDiff(diff string, opts Options) Report {
	report := Report{Findings: []Finding{}}
	if !opts.CodeBackendAvailable {
		report.Fallback = "diff_only"
	} else {
		report.CodeBackend = "available"
	}
	path := ""
	newLine := 0
	for _, raw := range strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(raw, "+++ "):
			path = strings.TrimSpace(strings.TrimPrefix(raw, "+++ "))
			path = strings.TrimPrefix(path, "b/")
		case strings.HasPrefix(raw, "@@"):
			newLine = parseNewLine(raw)
		case strings.HasPrefix(raw, "+") && !strings.HasPrefix(raw, "+++"):
			report.DiffLines++
			line := strings.TrimPrefix(raw, "+")
			report.Findings = append(report.Findings, analyzeAddedLine(path, newLine, line)...)
			if newLine > 0 {
				newLine++
			}
		case strings.HasPrefix(raw, " ") && newLine > 0:
			newLine++
		}
	}
	threshold := opts.LargeDiffLineThreshold
	if threshold <= 0 {
		threshold = 400
	}
	report.LargeDiff = report.DiffLines > threshold
	report.Summary = summarizeReport(report)
	return report
}

func analyzeAddedLine(path string, lineNo int, line string) []Finding {
	var findings []Finding
	if secretPattern.MatchString(line) {
		findings = append(findings, finding("secret-like-string", "high", path, lineNo, "Secret-like string added to the diff.", line))
	}
	if unsafeShellPattern.MatchString(line) {
		findings = append(findings, finding("unsafe-shell", "high", path, lineNo, "Unsafe shell execution pattern added.", line))
	}
	lower := strings.ToLower(line)
	if destructiveSQLPattern.MatchString(lower) || (strings.Contains(lower, "delete from") && !strings.Contains(lower, " where ")) {
		findings = append(findings, finding("destructive-sql", "high", path, lineNo, "Destructive SQL statement added.", line))
	}
	return findings
}

func finding(rule, severity, path string, line int, msg, snippet string) Finding {
	return Finding{
		RuleID:   rule,
		Severity: severity,
		Path:     path,
		Line:     line,
		Message:  msg,
		Snippet:  strings.TrimSpace(snippet),
	}
}

func parseNewLine(hunk string) int {
	idx := strings.Index(hunk, "+")
	if idx < 0 {
		return 0
	}
	tail := hunk[idx+1:]
	end := strings.IndexAny(tail, " ,")
	if end >= 0 {
		tail = tail[:end]
	}
	n, _ := strconv.Atoi(tail)
	return n
}

var (
	secretPattern         = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9._-]+|api[_-]?key\s*[:=]\s*["']?[^"'\s]+|token\s*[:=]\s*["']?[^"'\s]+)`)
	unsafeShellPattern    = regexp.MustCompile(`(?i)(rm\s+-rf\s+/|exec\.Command\("sh",\s*"-c"|exec\.Command\("bash",\s*"-c"|system\()`)
	destructiveSQLPattern = regexp.MustCompile(`(?i)(drop\s+table|drop\s+database|\btruncate\b)`)
)
