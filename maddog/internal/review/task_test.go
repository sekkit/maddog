package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedFileCodeContextReadsChangedFileSymbols(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "app", "config.go")
	if err := os.WriteFile(path, []byte("package app\n\nfunc LoadConfig() string {\n\treturn \"ok\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := AnalyzeUnifiedDiff("diff --git a/app/config.go b/app/config.go\n@@ -2,0 +3,1 @@\n+var token = \"example-token-value-for-redaction\"\n", Options{})

	got := ChangedFileCodeContext(root, report)
	if len(got) == 0 {
		t.Fatal("expected code context for changed file")
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"app/config.go", "LoadConfig", "package app"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("code context missing %q:\n%s", want, joined)
		}
	}
}

func TestChangedFileCodeContextFallsBackToFileNames(t *testing.T) {
	report := AnalyzeUnifiedDiff("diff --git a/app/missing.go b/app/missing.go\n@@ -1,0 +1,1 @@\n+var token = \"example-token-value-for-redaction\"\n", Options{})
	got := ChangedFileCodeContext(t.TempDir(), report)
	if len(got) != 1 || !strings.Contains(got[0], "changed file with deterministic finding: app/missing.go") {
		t.Fatalf("fallback context = %+v", got)
	}
}
