package skill

import (
	"strings"
	"testing"
)

// The improve advisor ships as a built-in so a fresh install has it with no
// companion files on disk; the store with no project root must serve it.
func TestBuiltinImproveSkillAvailableOnFreshInstall(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir()})
	var improve *Skill
	skills := store.List()
	for i := range skills {
		if skills[i].Name == "improve" {
			improve = &skills[i]
			break
		}
	}
	if improve == nil {
		t.Fatal("improve builtin not listed on a fresh install")
	}
	if improve.Scope != ScopeBuiltin {
		t.Errorf("scope = %s, want builtin", improve.Scope)
	}
	if improve.RunAs != RunInline {
		t.Errorf("runAs = %s, want inline (must orchestrate subagents and ask the user)", improve.RunAs)
	}

	// Self-containment: the references travel as appendices, and no instruction
	// points at companion files that don't exist for a builtin.
	for _, want := range []string{
		"Appendix A — Audit playbook",
		"Appendix B — Closing the loop",
		"Appendix C — Handoff plan template",
		"Finding format",
	} {
		if !strings.Contains(improve.Body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(improve.Body, "references/audit-playbook.md") ||
		strings.Contains(improve.Body, "references/plan-template.md") ||
		strings.Contains(improve.Body, "references/closing-the-loop.md") {
		t.Error("body still points at on-disk references files")
	}

	// Maddog adaptations: subagent fan-out, configurable concurrency, ask-based
	// selection, and worktree-isolated execute dispatch.
	for _, want := range []string{
		"read_only_task",
		"max_parallel_tools",
		"`ask` tool",
		"git worktree add",
	} {
		if !strings.Contains(improve.Body, want) {
			t.Errorf("body missing maddog adaptation %q", want)
		}
	}
}
