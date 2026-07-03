package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunBenchmarkAggregatesMockBackendResults(t *testing.T) {
	clock := &stepClock{t: time.Unix(100, 0)}
	backend := &fakeBenchBackend{
		id:   "mock",
		name: "MockGraph",
		caps: BackendCapabilities{SymbolSearch: true, SemanticSearch: true},
		results: map[string][]BenchmarkResult{
			"find runner": {
				{ID: "runner", Title: "RunBenchmark", Content: "func RunBenchmark(...)"},
				{ID: "other", Title: "Other", Content: "unrelated"},
			},
		},
	}

	report := RunBenchmark(context.Background(), BenchmarkOptions{
		Root:     t.TempDir(),
		Now:      clock.Now,
		Backends: []BenchmarkBackend{backend},
		Cases: []BenchmarkCase{{
			Name:         "symbol search",
			Query:        "find runner",
			Capability:   BenchmarkCapabilitySymbolSearch,
			ExpectedIDs:  []string{"runner"},
			TopK:         2,
			BudgetTokens: 128,
		}},
	})

	if len(report.Backends) != 1 {
		t.Fatalf("backends = %+v, want one mock backend", report.Backends)
	}
	got := report.Backends[0]
	if got.ID != "mock" || got.Name != "MockGraph" || got.Failures != 0 {
		t.Fatalf("backend summary = %+v, want successful mock", got)
	}
	if got.IndexBuildMillis == 0 || got.IncrementalUpdateMillis == 0 {
		t.Fatalf("index timings not recorded: %+v", got)
	}
	if len(got.Queries) != 1 || got.Queries[0].Status != BenchmarkQueryOK {
		t.Fatalf("query summary = %+v, want one ok query", got.Queries)
	}
	if got.Queries[0].RelevanceScore != 1 || got.Queries[0].ReturnedChars == 0 || got.Queries[0].EstimatedTokens == 0 || got.Queries[0].LatencyMillis == 0 {
		t.Fatalf("query metrics = %+v, want relevance, chars, latency", got.Queries[0])
	}
}

func TestRunBenchmarkSkipsUnsupportedCapability(t *testing.T) {
	backend := &fakeBenchBackend{id: "mock", name: "MockGraph", caps: BackendCapabilities{SymbolSearch: true}}

	report := RunBenchmark(context.Background(), BenchmarkOptions{
		Root:     t.TempDir(),
		Backends: []BenchmarkBackend{backend},
		Cases: []BenchmarkCase{{
			Name:       "semantic search",
			Query:      "architecture",
			Capability: BenchmarkCapabilitySemanticSearch,
		}},
	})

	got := report.Backends[0].Queries[0]
	if got.Status != BenchmarkQueryUnsupported {
		t.Fatalf("query status = %q, want unsupported: %+v", got.Status, got)
	}
	if backend.queryCalls != 0 {
		t.Fatalf("unsupported semantic search should not call backend, got %d calls", backend.queryCalls)
	}
}

func TestRunBenchmarkRecordsFailuresAndContinues(t *testing.T) {
	backend := &fakeBenchBackend{
		id:   "mock",
		name: "MockGraph",
		caps: BackendCapabilities{SymbolSearch: true},
		errs: map[string]error{"bad": errors.New("query boom")},
		results: map[string][]BenchmarkResult{
			"good": {{ID: "ok", Content: "result"}},
		},
	}

	report := RunBenchmark(context.Background(), BenchmarkOptions{
		Root:     t.TempDir(),
		Backends: []BenchmarkBackend{backend},
		Cases: []BenchmarkCase{
			{Name: "bad query", Query: "bad", Capability: BenchmarkCapabilitySymbolSearch},
			{Name: "good query", Query: "good", Capability: BenchmarkCapabilitySymbolSearch},
		},
	})

	got := report.Backends[0]
	if got.Failures != 1 {
		t.Fatalf("failures = %d, want 1: %+v", got.Failures, got)
	}
	if got.Health != BackendHealthDegraded {
		t.Fatalf("health = %q, want degraded after query failure", got.Health)
	}
	if got.Queries[0].Status != BenchmarkQueryError || !strings.Contains(got.Queries[0].Error, "query boom") {
		t.Fatalf("first query = %+v, want recorded error", got.Queries[0])
	}
	if got.Queries[1].Status != BenchmarkQueryOK {
		t.Fatalf("second query = %+v, want benchmark to continue", got.Queries[1])
	}
}

func TestSaveBenchmarkReportWritesJSONAndMarkdownSummary(t *testing.T) {
	report := BenchmarkReport{
		StartedAt: time.Unix(100, 0).UTC(),
		Root:      "repo",
		Backends: []BenchmarkBackendReport{{
			ID:      "mock",
			Name:    "MockGraph",
			Health:  BenchmarkHealthReady,
			Queries: []BenchmarkQueryReport{{Name: "symbol", Status: BenchmarkQueryOK, RelevanceScore: 1}},
		}},
	}

	saved, err := SaveBenchmarkReport(report, t.TempDir())
	if err != nil {
		t.Fatalf("SaveBenchmarkReport: %v", err)
	}
	if saved.JSONPath == "" || saved.MarkdownPath == "" {
		t.Fatalf("saved paths missing: %+v", saved)
	}
	if _, err := os.Stat(saved.JSONPath); err != nil {
		t.Fatalf("json report missing: %v", err)
	}
	md, err := os.ReadFile(saved.MarkdownPath)
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}
	if body := string(md); !strings.Contains(body, "MockGraph") || !strings.Contains(body, "symbol") || !strings.Contains(body, "Est. tokens") {
		t.Fatalf("markdown summary missing backend/query:\n%s", body)
	}
	latest := filepath.Join(filepath.Dir(saved.JSONPath), BenchmarkLatestJSONName)
	if _, err := os.Stat(latest); err != nil {
		t.Fatalf("latest json missing: %v", err)
	}
}

func TestSaveBenchmarkReportDoesNotOverwriteSameTimestampArchive(t *testing.T) {
	report := BenchmarkReport{StartedAt: time.Unix(100, 0).UTC()}
	dir := t.TempDir()
	benchDir := filepath.Join(dir, "codeintel-bench")
	if err := os.MkdirAll(benchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existingMarkdown := filepath.Join(benchDir, "bench-19700101-000140.000000000.md")
	if err := os.WriteFile(existingMarkdown, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := SaveBenchmarkReport(report, dir)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	second, err := SaveBenchmarkReport(report, dir)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if first.JSONPath == second.JSONPath || first.MarkdownPath == second.MarkdownPath {
		t.Fatalf("same timestamp saves should keep separate archives: first=%+v second=%+v", first, second)
	}
	if got, err := os.ReadFile(existingMarkdown); err != nil || string(got) != "keep me" {
		t.Fatalf("pre-existing markdown archive overwritten: got %q err %v", got, err)
	}
}

func TestLocalFilesBenchmarkBackendHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runner.go"), []byte("package runner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	backend := NewLocalFilesBenchmarkBackend()
	if err := backend.BuildIndex(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildIndex error = %v, want context.Canceled", err)
	}
}

func TestRunBenchmarkScoresExpectedIDsFromResultContent(t *testing.T) {
	backend := &fakeBenchBackend{
		id:   "codegraph",
		name: "CodeGraph",
		caps: BackendCapabilities{SymbolSearch: true},
		results: map[string][]BenchmarkResult{
			"RunBenchmark": {{ID: "codegraph_search", Title: "RunBenchmark", Content: "runner.go: func RunBenchmark()"}},
		},
	}

	report := RunBenchmark(context.Background(), BenchmarkOptions{
		Root:     t.TempDir(),
		Backends: []BenchmarkBackend{backend},
		Cases: []BenchmarkCase{{
			Name:        "symbol search",
			Query:       "RunBenchmark",
			Capability:  BenchmarkCapabilitySymbolSearch,
			ExpectedIDs: []string{"runner.go"},
			TopK:        5,
		}},
	})

	if got := report.Backends[0].Queries[0].RelevanceScore; got != 1 {
		t.Fatalf("relevance = %v, want expected ID matched from result content", got)
	}
}

func TestRunBenchmarkEstimatesCJKTokensFromContent(t *testing.T) {
	results := []BenchmarkResult{{ID: "zh.md", Title: "中文", Content: strings.Repeat("测试", 20)}}
	chars := benchmarkReturnedChars(results)
	tokens := estimateBenchmarkTokens(results)
	if tokens <= chars/4 {
		t.Fatalf("CJK estimate tokens = %d for %d chars, want above 4 chars/token heuristic", tokens, chars)
	}
}

type fakeBenchBackend struct {
	id         string
	name       string
	caps       BackendCapabilities
	results    map[string][]BenchmarkResult
	errs       map[string]error
	queryCalls int
}

func (f *fakeBenchBackend) BenchmarkInfo() BenchmarkBackendInfo {
	return BenchmarkBackendInfo{ID: f.id, Name: f.name, Capabilities: f.caps}
}

func (f *fakeBenchBackend) BuildIndex(context.Context, string) error { return nil }

func (f *fakeBenchBackend) UpdateIndex(context.Context, string) error { return nil }

func (f *fakeBenchBackend) Query(_ context.Context, query BenchmarkQuery) ([]BenchmarkResult, error) {
	f.queryCalls++
	if err := f.errs[query.Text]; err != nil {
		return nil, err
	}
	return f.results[query.Text], nil
}

type stepClock struct {
	t time.Time
}

func (c *stepClock) Now() time.Time {
	c.t = c.t.Add(10 * time.Millisecond)
	return c.t
}
