package codegraph

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var goFuncPattern = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// DefaultBenchmarkCases builds portable benchmark targets from the repository
// under test instead of assuming Maddog-specific file names and symbols.
func DefaultBenchmarkCases(root string) []BenchmarkCase {
	var cases []BenchmarkCase
	if symbol, path := firstGoSymbolCase(root); symbol != "" && path != "" {
		cases = append(cases, BenchmarkCase{
			Name:        "symbol search",
			Query:       symbol,
			Capability:  BenchmarkCapabilitySymbolSearch,
			ExpectedIDs: []string{path},
			TopK:        5,
		})
	}
	if query, path := firstSemanticCase(root); query != "" && path != "" {
		cases = append(cases, BenchmarkCase{
			Name:        "semantic context",
			Query:       query,
			Capability:  BenchmarkCapabilitySemanticSearch,
			ExpectedIDs: []string{path},
			TopK:        5,
		})
	}
	if len(cases) == 0 {
		cases = append(cases, BenchmarkCase{
			Name:       "repository search",
			Query:      "README",
			Capability: BenchmarkCapabilitySemanticSearch,
			TopK:       5,
		})
	}
	return cases
}

func firstGoSymbolCase(root string) (string, string) {
	var symbol, rel string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || symbol != "" {
			return nil
		}
		if d.IsDir() {
			if skipBenchmarkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		m := goFuncPattern.FindSubmatch(data)
		if len(m) < 2 {
			return nil
		}
		r, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		symbol = string(m[1])
		rel = filepath.ToSlash(r)
		return nil
	})
	return symbol, rel
}

func firstSemanticCase(root string) (string, string) {
	var query, rel string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || query != "" {
			return nil
		}
		if d.IsDir() {
			if skipBenchmarkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return nil
		}
		q := firstHeadingOrLine(text)
		if q == "" {
			return nil
		}
		r, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		query = q
		rel = filepath.ToSlash(r)
		return nil
	})
	return query, rel
}

func firstHeadingOrLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if line != "" {
				return line
			}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 80 {
				line = line[:80]
			}
			return line
		}
	}
	return ""
}

func skipBenchmarkDir(name string) bool {
	switch name {
	case ".git", ".maddog", "node_modules", "vendor", "dist", "build", "target", ".next":
		return true
	default:
		return false
	}
}
