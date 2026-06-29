package cli

import (
	"strings"
	"testing"
)

func TestBuildReviewTask(t *testing.T) {
	// Small diff.
	diff := "diff --git a/foo.go b/foo.go\n+added line"
	got := buildReviewTask(diff, "")
	if !strings.Contains(got, "Review the following changes.") {
		t.Error("missing review prompt prefix")
	}
	if !strings.Contains(got, "Deterministic review summary") {
		t.Error("missing deterministic review summary")
	}
	if !strings.Contains(got, diff) {
		t.Errorf("diff content missing:\n%s", got)
	}

	// With extra instructions.
	got = buildReviewTask(diff, "focus on error handling")
	if !strings.Contains(got, "focus on error handling") {
		t.Error("extra instructions missing")
	}
	if !strings.Contains(got, "The diff is:") {
		t.Error("missing diff separator")
	}

	// Truncation.
	hugeDiff := strings.Repeat("x", 20000)
	got = buildReviewTask(hugeDiff, "")
	if !strings.Contains(got, "truncated at 16000") {
		t.Error("large diff should be truncated")
	}
	if len(got) > 17000 {
		t.Errorf("truncated output too long: %d", len(got))
	}
}

func TestBuildReviewTaskInjectsRuleFindingsAndRedactsSecrets(t *testing.T) {
	diff := "diff --git a/app/config.go b/app/config.go\n@@ -1,0 +1,1 @@\n+token = \"example-token-value-for-redaction\"\n"
	got := buildReviewTask(diff, "")
	for _, want := range []string{"secret-like-token", "P1", "untrusted evidence", "changed file with deterministic finding"} {
		if !strings.Contains(got, want) {
			t.Fatalf("review task missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "example-token-value-for-redaction") {
		t.Fatalf("review task leaked secret:\n%s", got)
	}
}
