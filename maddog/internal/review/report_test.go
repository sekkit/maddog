package review

import (
	"strings"
	"testing"
)

func TestReportSummarizesLargeDiff(t *testing.T) {
	diff := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1,4 @@\n+a\n+b\n+c\n+d\n"
	report := AnalyzeDiff(diff, Options{LargeDiffLineThreshold: 2})

	if !report.LargeDiff || !strings.Contains(report.Summary, "large diff") {
		t.Fatalf("large diff summary = %+v", report)
	}
}

func TestReportHasDeterministicNoFindingSummary(t *testing.T) {
	report := AnalyzeDiff(`diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1,2 @@
+safe documentation
`, Options{CodeBackendAvailable: true})

	if len(report.Findings) != 0 {
		t.Fatalf("unexpected findings = %+v", report.Findings)
	}
	if report.Summary != "No deterministic review findings." {
		t.Fatalf("summary = %q", report.Summary)
	}
}
