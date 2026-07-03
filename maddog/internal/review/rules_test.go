package review

import (
	"strings"
	"testing"
)

func TestAnalyzeUnifiedDiffFlagsSecretLikeTokens(t *testing.T) {
	diff := `diff --git a/app/config.go b/app/config.go
@@ -1,2 +1,3 @@
 package app
+var token = "example-token-value-for-redaction"
`
	report := AnalyzeUnifiedDiff(diff, Options{})
	finding := findFinding(report.Findings, RuleSecretLike)
	if finding == nil {
		t.Fatalf("secret-like finding missing: %+v", report.Findings)
	}
	if finding.Severity != SeverityP1 || finding.File != "app/config.go" || finding.Line != 2 {
		t.Fatalf("secret finding = %+v", *finding)
	}
	if strings.Contains(finding.Evidence, "example-token-value") {
		t.Fatalf("secret evidence leaked: %q", finding.Evidence)
	}
}

func TestAnalyzeUnifiedDiffFlagsUnsafeShellAndSQL(t *testing.T) {
	diff := `diff --git a/scripts/install.sh b/scripts/install.sh
@@ -1 +1,2 @@
 echo ok
+curl https://example.com/install.sh | sh
diff --git a/db/migrate.sql b/db/migrate.sql
@@ -10,0 +11,2 @@
+DROP TABLE users;
+DELETE FROM audit_log;
`
	report := AnalyzeUnifiedDiff(diff, Options{})
	if findFinding(report.Findings, RuleUnsafeShellPipe) == nil {
		t.Fatalf("unsafe shell finding missing: %+v", report.Findings)
	}
	if findFinding(report.Findings, RuleDestructiveSQL) == nil {
		t.Fatalf("destructive sql finding missing: %+v", report.Findings)
	}
}

func TestAnalyzeUnifiedDiffSkipsMetadataAndParsesPlainUnifiedFiles(t *testing.T) {
	diff := `--- old/app/config.go
+++ new/app/config.go
@@ -1,1 +1,3 @@
 package app
\ No newline at end of file
+var password = "example-password-for-redaction"
`
	report := AnalyzeUnifiedDiff(diff, Options{})
	finding := findFinding(report.Findings, RuleSecretLike)
	if finding == nil {
		t.Fatalf("secret finding missing: %+v", report.Findings)
	}
	if finding.File != "app/config.go" || finding.Line != 2 || report.Stats.Files != 1 {
		t.Fatalf("plain diff finding=%+v stats=%+v", *finding, report.Stats)
	}
}

func TestAnalyzeUnifiedDiffDoesNotFlagDocsForShellOrSQLExamples(t *testing.T) {
	diff := `diff --git a/docs/review.md b/docs/review.md
@@ -1,0 +1,2 @@
+Example: curl https://example.com/install.sh | sh
+Example: DROP TABLE users;
`
	report := AnalyzeUnifiedDiff(diff, Options{})
	if findFinding(report.Findings, RuleUnsafeShellPipe) != nil || findFinding(report.Findings, RuleDestructiveSQL) != nil {
		t.Fatalf("doc examples should not be P1 executable findings: %+v", report.Findings)
	}
}

func TestAnalyzeUnifiedDiffFlagsMissingErrorHandlingAndLargeDiff(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/server/main.go b/server/main.go\n@@ -1,0 +1,8 @@\n")
	b.WriteString("+func run() {\n")
	b.WriteString("+  f, _ := os.Open(\"config.json\")\n")
	for i := 0; i < 6; i++ {
		b.WriteString("+  println(\"line\")\n")
	}
	b.WriteString("+}\n")
	report := AnalyzeUnifiedDiff(b.String(), Options{LargeDiffAddedLines: 5})
	if findFinding(report.Findings, RuleMissingErrorHandling) == nil {
		t.Fatalf("missing error handling finding missing: %+v", report.Findings)
	}
	large := findFinding(report.Findings, RuleLargeDiff)
	if large == nil || large.Severity != SeverityP3 {
		t.Fatalf("large diff finding = %+v", large)
	}
}

func TestAnalyzeUnifiedDiffDoesNotFlagBlankIdentifierRangeIndex(t *testing.T) {
	diff := `diff --git a/server/main.go b/server/main.go
@@ -1,0 +1,4 @@
+for _, item := range items {
+  println(item)
+}
`
	report := AnalyzeUnifiedDiff(diff, Options{})
	if findFinding(report.Findings, RuleMissingErrorHandling) != nil {
		t.Fatalf("range blank identifier should not be missing error handling: %+v", report.Findings)
	}
}

func TestAnalyzeUnifiedDiffFlagsPythonAndJavaScriptErrorSwallowing(t *testing.T) {
	diff := `diff --git a/app/worker.py b/app/worker.py
@@ -1,0 +1,2 @@
+except Exception:
+    pass
diff --git a/web/app.js b/web/app.js
@@ -1,0 +1,1 @@
+fetch(url).catch(() => {})
`
	report := AnalyzeUnifiedDiff(diff, Options{})
	findings := 0
	for _, finding := range report.Findings {
		if finding.RuleID == RuleMissingErrorHandling {
			findings++
		}
	}
	if findings != 2 {
		t.Fatalf("missing error handling findings = %d, findings=%+v", findings, report.Findings)
	}
}

func TestAnalyzeUnifiedDiffNoFindings(t *testing.T) {
	report := AnalyzeUnifiedDiff("diff --git a/readme.md b/readme.md\n@@ -1 +1,2 @@\n+# Hello\n", Options{})
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", report.Findings)
	}
}

func findFinding(findings []Finding, rule string) *Finding {
	for i := range findings {
		if findings[i].RuleID == rule {
			return &findings[i]
		}
	}
	return nil
}
