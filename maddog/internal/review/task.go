package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func BuildTask(diff string, extra string, codeIntelligence []string) string {
	report := AnalyzeUnifiedDiff(diff, Options{})
	safeDiff := RedactSecrets(diff)
	rulePrompt := BuildLLMPrompt(report, PromptOptions{
		Task:                "review pending changes",
		CodeIntelligence:    codeIntelligence,
		MaxFindingEvidence:  160,
		MaxContextFragments: 6,
	})
	var b strings.Builder
	b.WriteString("Review the following changes. ")
	if strings.TrimSpace(extra) != "" {
		b.WriteString(strings.TrimSpace(extra))
		b.WriteString(" ")
	}
	b.WriteString("First use this deterministic rules report as grounded input, then explain/prioritize it and add only high-confidence diff-backed findings.\n\n")
	b.WriteString(rulePrompt)
	b.WriteString("\n\n")
	b.WriteString("The diff is:\n\n```diff\n")
	const maxLen = 16000
	if len(safeDiff) > maxLen {
		b.WriteString(safeDiff[:maxLen])
		b.WriteString("\n```\n\n(diff truncated at ")
		fmt.Fprint(&b, maxLen)
		b.WriteString(" chars; focus on the changes shown)")
	} else {
		b.WriteString(safeDiff)
		b.WriteString("\n```")
	}
	return b.String()
}

func ChangedFileContext(report Report) []string {
	if len(report.Findings) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, finding := range report.Findings {
		if finding.File == "" || seen[finding.File] {
			continue
		}
		seen[finding.File] = true
		out = append(out, "changed file with deterministic finding: "+finding.File)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func ChangedFileCodeContext(root string, report Report) []string {
	fallback := ChangedFileContext(report)
	if len(report.Findings) == 0 {
		return nil
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return fallback
	}
	seen := map[string]bool{}
	out := []string{}
	for _, finding := range report.Findings {
		if finding.File == "" || seen[finding.File] {
			continue
		}
		seen[finding.File] = true
		fragment := fileContextFragment(root, finding.File)
		if fragment == "" {
			continue
		}
		out = append(out, fragment)
		if len(out) >= 6 {
			break
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func fileContextFragment(root, rel string) string {
	cleanRel := filepath.Clean(strings.TrimSpace(rel))
	if cleanRel == "." || strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return ""
	}
	path := filepath.Join(root, cleanRel)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 80 {
		lines = lines[:80]
	}
	var b strings.Builder
	b.WriteString("changed file code context: ")
	b.WriteString(filepath.ToSlash(cleanRel))
	b.WriteString("\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if i < 12 || looksLikeSymbolLine(trimmed) {
			b.WriteString(fmt.Sprintf("%d: %s\n", i+1, trimmed))
		}
		if b.Len() > 1400 {
			b.WriteString("[truncated]\n")
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func looksLikeSymbolLine(line string) bool {
	return strings.HasPrefix(line, "func ") ||
		strings.HasPrefix(line, "class ") ||
		strings.HasPrefix(line, "def ") ||
		strings.HasPrefix(line, "function ") ||
		strings.Contains(line, " function(") ||
		strings.Contains(line, "=>")
}
