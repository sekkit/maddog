package review

import (
	"strings"
	"testing"
)

func TestBuildLLMPromptIncludesDeterministicFindingsAndCodeContext(t *testing.T) {
	report := Report{
		Stats: Stats{Files: 1, AddedLines: 1},
		Findings: []Finding{{
			RuleID:   RuleSecretLike,
			Severity: SeverityP1,
			File:     "app/config.go",
			Line:     2,
			Message:  "secret-like token added",
			Evidence: "var token = ...",
		}},
	}
	prompt := BuildLLMPrompt(report, PromptOptions{
		Task:                "review pending changes",
		CodeIntelligence:    []string{"symbol Config.Load is called by cmd/server"},
		MaxFindingEvidence:  80,
		MaxContextFragments: 3,
	})
	for _, want := range []string{"review pending changes", "P1", RuleSecretLike, "app/config.go:2", "symbol Config.Load"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "untrusted evidence") || !strings.Contains(prompt, "```text") {
		t.Fatalf("prompt should fence untrusted evidence/context:\n%s", prompt)
	}
}

func TestBuildLLMPromptHandlesNoFindingsAndLargeDiff(t *testing.T) {
	report := Report{Stats: Stats{Files: 12, AddedLines: 700, DeletedLines: 120, Large: true}}
	prompt := BuildLLMPrompt(report, PromptOptions{Task: "review branch"})
	for _, want := range []string{"No deterministic findings", "large diff", "12 files", "fallback diff-only"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
