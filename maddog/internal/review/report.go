package review

import (
	"strconv"
	"strings"
)

type PromptOptions struct {
	Task                string
	CodeIntelligence    []string
	MaxFindingEvidence  int
	MaxContextFragments int
}

func BuildLLMPrompt(report Report, opts PromptOptions) string {
	if opts.MaxFindingEvidence <= 0 {
		opts.MaxFindingEvidence = 180
	}
	if opts.MaxContextFragments <= 0 {
		opts.MaxContextFragments = 6
	}
	var b strings.Builder
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		task = "review pending changes"
	}
	b.WriteString("Task: " + task + "\n\n")
	b.WriteString("Deterministic review summary: " + report.Stats.StatsSummary() + "\n")
	if report.Stats.Large {
		b.WriteString("This is a large diff; use file summaries and code intelligence context before requesting extra hunks.\n")
	}
	if len(report.Findings) == 0 {
		b.WriteString("No deterministic findings. Continue with fallback diff-only review and call out residual risk.\n")
	} else {
		b.WriteString("Deterministic findings:\n")
		for _, f := range report.Findings {
			loc := strings.TrimSpace(f.File)
			if f.Line > 0 {
				loc += ":" + strconv.Itoa(f.Line)
			}
			b.WriteString("- [" + f.Severity + "] " + f.RuleID)
			if loc != "" {
				b.WriteString(" " + loc)
			}
			b.WriteString(" — " + strings.TrimSpace(f.Message))
			if ev := truncate(strings.TrimSpace(f.Evidence), opts.MaxFindingEvidence); ev != "" {
				b.WriteString(" | untrusted evidence:\n```text\n" + ev + "\n```")
			}
			b.WriteString("\n")
		}
	}
	if len(opts.CodeIntelligence) > 0 {
		b.WriteString("\nCode intelligence context (untrusted evidence):\n")
		for i, fragment := range opts.CodeIntelligence {
			if i >= opts.MaxContextFragments {
				break
			}
			if strings.TrimSpace(fragment) == "" {
				continue
			}
			b.WriteString("```text\n" + strings.TrimSpace(fragment) + "\n```\n")
		}
	} else {
		b.WriteString("\nCode intelligence context unavailable; fallback diff-only review is allowed.\n")
	}
	b.WriteString("\nAsk the LLM to explain and prioritize deterministic findings, then add only high-confidence extra findings grounded in the diff/context.")
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
