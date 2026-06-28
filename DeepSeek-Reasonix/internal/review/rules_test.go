package review

import "testing"

func TestAnalyzeDiffFindsSecretUnsafeShellAndDestructiveSQL(t *testing.T) {
	report := AnalyzeDiff(`diff --git a/app.go b/app.go
--- a/app.go
+++ b/app.go
@@ -1,3 +1,6 @@
+api_key := "sk-live-secret"
+cmd := exec.Command("sh", "-c", userInput)
+db.Exec("DROP TABLE users")
`, Options{})

	for _, rule := range []string{"secret-like-string", "unsafe-shell", "destructive-sql"} {
		if !hasRule(report.Findings, rule) {
			t.Fatalf("missing rule %q in %+v", rule, report.Findings)
		}
	}
}

func TestAnalyzeDiffUsesDiffOnlyFallbackWhenCodeBackendMissing(t *testing.T) {
	report := AnalyzeDiff(`diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
+hello
`, Options{CodeBackendAvailable: false})

	if report.Fallback != "diff_only" {
		t.Fatalf("fallback = %q, want diff_only", report.Fallback)
	}
}

func hasRule(findings []Finding, rule string) bool {
	for _, finding := range findings {
		if finding.RuleID == rule {
			return true
		}
	}
	return false
}
