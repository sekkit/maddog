package codegraph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultBenchmarkCasesAreGeneratedFromTargetRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.go"), []byte("package alpha\nfunc PortableSymbol() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "notes.md"), []byte("# Portable Architecture\n\nThis repository documents a portable search target.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := DefaultBenchmarkCases(root)
	if len(cases) < 2 {
		t.Fatalf("DefaultBenchmarkCases returned %d cases, want symbol and semantic cases: %+v", len(cases), cases)
	}
	seen := map[string]BenchmarkCase{}
	for _, tc := range cases {
		seen[tc.Capability] = tc
		for _, bad := range append([]string{tc.Query}, tc.ExpectedIDs...) {
			if bad == "RunBenchmark" || bad == "runner.go" || bad == "docs/cc/maddog-fusion--3949/tech.md" {
				t.Fatalf("generated case still uses Maddog-specific fixture %+v", tc)
			}
		}
	}
	if got := seen[BenchmarkCapabilitySymbolSearch]; got.Query != "PortableSymbol" || len(got.ExpectedIDs) != 1 || got.ExpectedIDs[0] != "alpha.go" {
		t.Fatalf("symbol case = %+v, want PortableSymbol in alpha.go", got)
	}
	if got := seen[BenchmarkCapabilitySemanticSearch]; got.Query != "Portable Architecture" || len(got.ExpectedIDs) != 1 || got.ExpectedIDs[0] != filepath.ToSlash(filepath.Join("docs", "notes.md")) {
		t.Fatalf("semantic case = %+v, want docs/notes.md heading", got)
	}
}
