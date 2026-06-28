package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunBenchmarkReportsJSONAndMarkdownSummary(t *testing.T) {
	backend := &benchFakeBackend{
		id:           "mock",
		name:         "Mock Backend",
		capabilities: []BackendCapability{BackendCapabilitySymbolSearch, BackendCapabilitySemanticSearch},
		results: []BenchmarkSearchResult{{
			Path:    "internal/loop/template.go",
			Line:    12,
			Symbol:  "LoopTemplateV1",
			Snippet: "type LoopTemplateV1 struct {}",
		}},
	}

	report, err := RunBenchmark(context.Background(), BenchmarkOptions{
		Root:     ".",
		Backends: []BenchmarkBackend{backend},
		Queries: []BenchmarkQuery{{
			ID:    "loop-template",
			Kind:  BenchmarkQuerySymbol,
			Query: "LoopTemplateV1",
			TopK:  3,
			Want:  []string{"LoopTemplateV1"},
		}},
		Now: time.Date(2026, 6, 28, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RunBenchmark: %v", err)
	}
	if len(report.Backends) != 1 {
		t.Fatalf("Backends len = %d, want 1", len(report.Backends))
	}
	got := report.Backends[0]
	if got.BackendID != "mock" || got.Health != BackendHealthAvailable {
		t.Fatalf("backend summary = %+v", got)
	}
	if got.IndexBuildMS < 0 || got.IncrementalUpdateMS < 0 || got.QueryLatencyMS < 0 {
		t.Fatalf("durations should be recorded in milliseconds: %+v", got)
	}
	if got.TopKRelevance != 1 || got.TokenCharsReturned == 0 || got.ToolFailures != 0 {
		t.Fatalf("metrics = %+v, want relevance 1, returned chars, no failures", got)
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"index_build_ms", "incremental_update_ms", "query_latency_ms", "top_k_relevance", "token_chars_returned", "tool_failures"} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("JSON report missing %q: %s", field, raw)
		}
	}
	md := report.Markdown()
	if !strings.Contains(md, "Code intelligence benchmark") || !strings.Contains(md, "mock") || !strings.Contains(md, "100%") {
		t.Fatalf("markdown summary missing benchmark data:\n%s", md)
	}
}

func TestRunBenchmarkReportsUnsupportedSemanticSearch(t *testing.T) {
	backend := &benchFakeBackend{
		id:           "symbol-only",
		name:         "Symbol Only",
		capabilities: []BackendCapability{BackendCapabilitySymbolSearch},
	}

	report, err := RunBenchmark(context.Background(), BenchmarkOptions{
		Backends: []BenchmarkBackend{backend},
		Queries: []BenchmarkQuery{{
			ID:    "semantic-refactor",
			Kind:  BenchmarkQuerySemantic,
			Query: "places that rewrite provider auth",
			TopK:  3,
			Want:  []string{"auth"},
		}},
	})
	if err != nil {
		t.Fatalf("RunBenchmark: %v", err)
	}
	got := report.Backends[0]
	if got.Unsupported != 1 || len(got.Cases) != 1 || !got.Cases[0].Unsupported {
		t.Fatalf("semantic case should be reported unsupported, not failed: %+v", got)
	}
	if got.ToolFailures != 0 {
		t.Fatalf("unsupported semantic search should not count as tool failure: %+v", got)
	}
}

func TestRunBenchmarkCountsQueryFailureWithoutAborting(t *testing.T) {
	backend := &benchFakeBackend{
		id:           "flaky",
		name:         "Flaky",
		capabilities: []BackendCapability{BackendCapabilitySymbolSearch},
		queryErr:     errors.New("search exploded"),
	}

	report, err := RunBenchmark(context.Background(), BenchmarkOptions{
		Backends: []BenchmarkBackend{backend},
		Queries: []BenchmarkQuery{
			{ID: "bad", Kind: BenchmarkQuerySymbol, Query: "missing", TopK: 1, Want: []string{"missing"}},
			{ID: "still-runs", Kind: BenchmarkQuerySymbol, Query: "again", TopK: 1, Want: []string{"again"}},
		},
	})
	if err != nil {
		t.Fatalf("RunBenchmark should keep query failures in the report, got error: %v", err)
	}
	got := report.Backends[0]
	if got.ToolFailures != 2 || len(got.Cases) != 2 {
		t.Fatalf("failures = %+v, want two recorded query failures", got)
	}
	for _, c := range got.Cases {
		if c.Failure == "" || c.Supported == false {
			t.Fatalf("case should be supported with recorded failure: %+v", c)
		}
	}
}

func TestRunBenchmarkReportsFastContextStyleCitationTrace(t *testing.T) {
	backend := &benchFakeBackend{
		id:           "fastcontext-style",
		name:         "FastContext Style",
		capabilities: []BackendCapability{BackendCapabilitySymbolSearch, BackendCapabilityContextPack},
		results: []BenchmarkSearchResult{
			{Path: "internal/loop/template.go", Line: 12, Symbol: "LoopTemplateV1", Snippet: "type LoopTemplateV1 struct {}"},
			{Path: "internal/loop/readiness.go", Line: 0, Symbol: "ReadinessResult", Snippet: "type ReadinessResult struct {}"},
		},
	}

	report, err := RunBenchmark(context.Background(), BenchmarkOptions{
		Backends: []BenchmarkBackend{backend},
		Queries: []BenchmarkQuery{{
			ID:    "fastcontext-citations",
			Kind:  BenchmarkQuerySymbol,
			Query: "LoopTemplateV1",
			TopK:  2,
			Want:  []string{"LoopTemplateV1", "ReadinessResult"},
		}},
	})
	if err != nil {
		t.Fatalf("RunBenchmark: %v", err)
	}
	got := report.Backends[0]
	if got.CitationPrecision != 0.5 {
		t.Fatalf("backend citation precision = %+v, want 0.5", got)
	}
	if len(got.Cases) != 1 || got.Cases[0].CitationPrecision != 0.5 {
		t.Fatalf("case citation precision = %+v", got.Cases)
	}
	if len(got.Cases[0].ExplorationTrace) != 2 || !strings.Contains(got.Cases[0].ExplorationTrace[0], "internal/loop/template.go:12") {
		t.Fatalf("exploration trace = %+v", got.Cases[0].ExplorationTrace)
	}
	if !strings.Contains(report.Markdown(), "Citation") {
		t.Fatalf("markdown should include citation precision:\n%s", report.Markdown())
	}
}

type benchFakeBackend struct {
	id           string
	name         string
	capabilities []BackendCapability
	health       BackendHealth
	buildErr     error
	updateErr    error
	queryErr     error
	results      []BenchmarkSearchResult
}

func (b *benchFakeBackend) ID() string { return b.id }

func (b *benchFakeBackend) Name() string { return b.name }

func (b *benchFakeBackend) Capabilities() []BackendCapability {
	return append([]BackendCapability(nil), b.capabilities...)
}

func (b *benchFakeBackend) Health() BackendHealth {
	if b.health != "" {
		return b.health
	}
	return BackendHealthAvailable
}

func (b *benchFakeBackend) BuildIndex(context.Context, string) error { return b.buildErr }

func (b *benchFakeBackend) UpdateIndex(context.Context, BenchmarkUpdate) error { return b.updateErr }

func (b *benchFakeBackend) Query(context.Context, BenchmarkQuery) ([]BenchmarkSearchResult, error) {
	return append([]BenchmarkSearchResult(nil), b.results...), b.queryErr
}
