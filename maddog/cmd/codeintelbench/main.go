package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"maddog/internal/codegraph"
	"maddog/internal/config"
)

func main() {
	repo := flag.String("repo", ".", "repository root to benchmark")
	backends := flag.String("backends", "builtin,mock", "comma-separated backends: builtin,mock")
	outMD := flag.String("out", "", "write markdown report here (default: stdout)")
	outJSON := flag.String("json", "", "write JSON report here (optional)")
	timeout := flag.Duration("timeout", 30*time.Second, "benchmark timeout")
	writeLatest := flag.Bool("latest", true, "write latest JSON report under Maddog cache for doctor")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "codeintelbench - Maddog code intelligence benchmark.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", flag.CommandLine.Name())
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  go run ./cmd/codeintelbench -repo . -out codeintel.md -json codeintel.json\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  go run ./cmd/codeintelbench -backends builtin\n")
	}
	flag.Parse()

	benchBackends, err := selectBackends(*backends)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	report, err := codegraph.RunBenchmark(ctx, codegraph.BenchmarkOptions{
		Root:     *repo,
		Backends: benchBackends,
		Queries:  codegraph.DefaultBenchmarkQueries(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchmark:", err)
		os.Exit(1)
	}
	if *outMD != "" {
		if err := codegraph.WriteBenchmarkReportMarkdown(*outMD, report); err != nil {
			fmt.Fprintln(os.Stderr, "write markdown:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(report.Markdown())
	}
	if *outJSON != "" {
		if err := codegraph.WriteBenchmarkReportJSON(*outJSON, report); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
			os.Exit(1)
		}
	}
	if *writeLatest {
		if _, err := codegraph.WriteLatestBenchmarkReport(config.CacheDir(), report); err != nil {
			fmt.Fprintln(os.Stderr, "write latest:", err)
			os.Exit(1)
		}
	}
}

func selectBackends(raw string) ([]codegraph.BenchmarkBackend, error) {
	var out []codegraph.BenchmarkBackend
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		switch item {
		case "builtin", "built-in", "codegraph", codegraph.BuiltInBackendID:
			out = append(out, codegraph.NewBuiltInBenchmarkBackend())
		case "mock":
			out = append(out, codegraph.NewMockBenchmarkBackend())
		default:
			return nil, fmt.Errorf("unknown code intelligence benchmark backend %q", item)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no code intelligence benchmark backends selected")
	}
	return out, nil
}
