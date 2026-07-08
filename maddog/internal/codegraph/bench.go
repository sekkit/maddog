package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"maddog/internal/fileutil"
)

const (
	BenchmarkCapabilitySymbolSearch   = "symbol_search"
	BenchmarkCapabilitySemanticSearch = "semantic_search"

	BenchmarkQueryOK          = "ok"
	BenchmarkQueryUnsupported = "unsupported"
	BenchmarkQueryError       = "error"

	BenchmarkHealthReady = "ready"

	BenchmarkLatestJSONName     = "latest.json"
	BenchmarkLatestMarkdownName = "latest.md"
)

type BenchmarkBackend interface {
	BenchmarkInfo() BenchmarkBackendInfo
	BuildIndex(context.Context, string) error
	UpdateIndex(context.Context, string) error
	Query(context.Context, BenchmarkQuery) ([]BenchmarkResult, error)
}

type BenchmarkBackendInfo struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Capabilities BackendCapabilities `json:"capabilities"`
	Health       string              `json:"health,omitempty"`
	SkipIndex    bool                `json:"skip_index,omitempty"`
}

type BenchmarkResult struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

type BenchmarkOptions struct {
	Root     string
	Backends []BenchmarkBackend
	Cases    []BenchmarkCase
	Now      func() time.Time
}

type BenchmarkCase struct {
	Name         string   `json:"name"`
	Query        string   `json:"query"`
	Capability   string   `json:"capability"`
	ExpectedIDs  []string `json:"expected_ids,omitempty"`
	TopK         int      `json:"top_k,omitempty"`
	BudgetTokens int      `json:"budget_tokens,omitempty"`
}

type BenchmarkQuery struct {
	Name         string `json:"name"`
	Text         string `json:"text"`
	Capability   string `json:"capability"`
	TopK         int    `json:"top_k,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type BenchmarkReport struct {
	StartedAt time.Time                `json:"started_at"`
	Root      string                   `json:"root"`
	Backends  []BenchmarkBackendReport `json:"backends"`
}

type BenchmarkBackendReport struct {
	ID                      string                 `json:"id"`
	Name                    string                 `json:"name"`
	Health                  string                 `json:"health"`
	Capabilities            BackendCapabilities    `json:"capabilities"`
	IndexBuildMillis        int64                  `json:"index_build_millis"`
	IncrementalUpdateMillis int64                  `json:"incremental_update_millis"`
	Failures                int                    `json:"failures"`
	Queries                 []BenchmarkQueryReport `json:"queries"`
}

type BenchmarkQueryReport struct {
	Name            string  `json:"name"`
	Query           string  `json:"query,omitempty"`
	Capability      string  `json:"capability,omitempty"`
	Status          string  `json:"status"`
	LatencyMillis   int64   `json:"latency_millis,omitempty"`
	RelevanceScore  float64 `json:"relevance_score,omitempty"`
	ReturnedChars   int     `json:"returned_chars,omitempty"`
	EstimatedTokens int     `json:"estimated_tokens,omitempty"`
	ResultCount     int     `json:"result_count,omitempty"`
	Error           string  `json:"error,omitempty"`
}

type SavedBenchmarkReport struct {
	JSONPath     string
	MarkdownPath string
}

type LocalFilesBenchmarkBackend struct {
	index []BenchmarkResult
}

func NewLocalFilesBenchmarkBackend() *LocalFilesBenchmarkBackend {
	return &LocalFilesBenchmarkBackend{}
}

func (b *LocalFilesBenchmarkBackend) BenchmarkInfo() BenchmarkBackendInfo {
	return BenchmarkBackendInfo{
		ID:   "local-files",
		Name: "Local file scan",
		Capabilities: BackendCapabilities{
			SymbolSearch: true,
		},
		Health: BenchmarkHealthReady,
	}
}

func (b *LocalFilesBenchmarkBackend) BuildIndex(ctx context.Context, root string) error {
	var results []BenchmarkResult
	if strings.TrimSpace(root) == "" {
		b.index = results
		return nil
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || d.IsDir() {
			return nil
		}
		if !benchmarkSourceFile(path) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		results = append(results, BenchmarkResult{
			ID:      filepath.ToSlash(rel),
			Title:   filepath.Base(path),
			Content: string(raw),
		})
		return nil
	})
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	b.index = results
	return err
}

func (b *LocalFilesBenchmarkBackend) UpdateIndex(ctx context.Context, root string) error {
	return b.BuildIndex(ctx, root)
}

func (b *LocalFilesBenchmarkBackend) Query(ctx context.Context, query BenchmarkQuery) ([]BenchmarkResult, error) {
	if query.Capability != BenchmarkCapabilitySymbolSearch {
		return nil, nil
	}
	needle := strings.ToLower(strings.TrimSpace(query.Text))
	var out []BenchmarkResult
	for _, item := range b.index {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		haystack := strings.ToLower(item.ID + "\n" + item.Title + "\n" + item.Content)
		if needle == "" || strings.Contains(haystack, needle) {
			out = append(out, item)
		}
	}
	if query.TopK > 0 && query.TopK < len(out) {
		out = out[:query.TopK]
	}
	return out, nil
}

func RunBenchmark(ctx context.Context, opts BenchmarkOptions) BenchmarkReport {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	report := BenchmarkReport{
		StartedAt: now().UTC(),
		Root:      opts.Root,
		Backends:  make([]BenchmarkBackendReport, 0, len(opts.Backends)),
	}
	for _, backend := range opts.Backends {
		info := backend.BenchmarkInfo()
		health := info.Health
		if strings.TrimSpace(health) == "" {
			health = BenchmarkHealthReady
		}
		backendReport := BenchmarkBackendReport{
			ID:           info.ID,
			Name:         info.Name,
			Health:       health,
			Capabilities: info.Capabilities,
			Queries:      make([]BenchmarkQueryReport, 0, len(opts.Cases)),
		}
		if closer, ok := backend.(interface{ Close() error }); ok {
			defer closer.Close()
		}

		var start time.Time
		if !info.SkipIndex {
			start = now()
			if err := backend.BuildIndex(ctx, opts.Root); err != nil {
				backendReport.Failures++
				backendReport.Health = BackendHealthDegraded
			}
			backendReport.IndexBuildMillis = elapsedMillis(start, now())

			start = now()
			if err := backend.UpdateIndex(ctx, opts.Root); err != nil {
				backendReport.Failures++
				backendReport.Health = BackendHealthDegraded
			}
			backendReport.IncrementalUpdateMillis = elapsedMillis(start, now())
		}

		for _, tc := range opts.Cases {
			qr := BenchmarkQueryReport{
				Name:       tc.Name,
				Query:      tc.Query,
				Capability: tc.Capability,
			}
			if !benchmarkCapabilitySupported(info.Capabilities, tc.Capability) {
				qr.Status = BenchmarkQueryUnsupported
				backendReport.Queries = append(backendReport.Queries, qr)
				continue
			}
			start = now()
			results, err := backend.Query(ctx, BenchmarkQuery{
				Name:         tc.Name,
				Text:         tc.Query,
				Capability:   tc.Capability,
				TopK:         tc.TopK,
				BudgetTokens: tc.BudgetTokens,
			})
			qr.LatencyMillis = elapsedMillis(start, now())
			if err != nil {
				qr.Status = BenchmarkQueryError
				qr.Error = err.Error()
				backendReport.Failures++
				backendReport.Health = BackendHealthDegraded
				backendReport.Queries = append(backendReport.Queries, qr)
				continue
			}
			qr.Status = BenchmarkQueryOK
			qr.ResultCount = len(results)
			qr.ReturnedChars = benchmarkReturnedChars(results)
			qr.EstimatedTokens = estimateBenchmarkTokens(results)
			qr.RelevanceScore = benchmarkRelevance(results, tc.ExpectedIDs, tc.TopK)
			backendReport.Queries = append(backendReport.Queries, qr)
		}
		report.Backends = append(report.Backends, backendReport)
	}
	return report
}

func SaveBenchmarkReport(report BenchmarkReport, dir string) (SavedBenchmarkReport, error) {
	if strings.TrimSpace(dir) == "" {
		return SavedBenchmarkReport{}, fmt.Errorf("benchmark report directory is required")
	}
	outDir := filepath.Join(dir, "codeintel-bench")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return SavedBenchmarkReport{}, err
	}
	stamp := report.StartedAt.UTC().Format("20060102-150405.000000000")
	if stamp == "00010101-000000.000000000" {
		stamp = time.Now().UTC().Format("20060102-150405.000000000")
	}
	jsonPath, markdownPath := uniqueBenchmarkArchivePaths(outDir, stamp)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return SavedBenchmarkReport{}, err
	}
	if err := writeFileAtomic(jsonPath, raw); err != nil {
		return SavedBenchmarkReport{}, err
	}
	if err := writeFileAtomic(filepath.Join(outDir, BenchmarkLatestJSONName), raw); err != nil {
		return SavedBenchmarkReport{}, err
	}
	markdown := []byte(RenderBenchmarkMarkdown(report))
	if err := writeFileAtomic(markdownPath, markdown); err != nil {
		return SavedBenchmarkReport{}, err
	}
	if err := writeFileAtomic(filepath.Join(outDir, BenchmarkLatestMarkdownName), markdown); err != nil {
		return SavedBenchmarkReport{}, err
	}
	return SavedBenchmarkReport{JSONPath: jsonPath, MarkdownPath: markdownPath}, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func uniqueBenchmarkArchivePaths(dir, stamp string) (string, string) {
	base := filepath.Join(dir, "bench-"+stamp)
	jsonPath := base + ".json"
	markdownPath := base + ".md"
	if !fileExists(jsonPath) && !fileExists(markdownPath) {
		return jsonPath, markdownPath
	}
	for i := 2; ; i++ {
		jsonPath = fmt.Sprintf("%s-%d.json", base, i)
		markdownPath = fmt.Sprintf("%s-%d.md", base, i)
		if !fileExists(jsonPath) && !fileExists(markdownPath) {
			return jsonPath, markdownPath
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func RenderBenchmarkMarkdown(report BenchmarkReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code Intelligence Benchmark\n\n")
	fmt.Fprintf(&b, "- Started: %s\n", report.StartedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Root: %s\n\n", valueOrBenchmark(report.Root, "(not set)"))
	for _, backend := range report.Backends {
		fmt.Fprintf(&b, "## %s\n\n", valueOrBenchmark(backend.Name, backend.ID))
		fmt.Fprintf(&b, "- Health: %s\n", valueOrBenchmark(backend.Health, "unknown"))
		fmt.Fprintf(&b, "- Index build: %dms\n", backend.IndexBuildMillis)
		fmt.Fprintf(&b, "- Incremental update: %dms\n", backend.IncrementalUpdateMillis)
		fmt.Fprintf(&b, "- Failures: %d\n\n", backend.Failures)
		if len(backend.Queries) == 0 {
			fmt.Fprintf(&b, "No query cases.\n\n")
			continue
		}
		fmt.Fprintf(&b, "| Query | Status | Latency | Relevance | Returned chars | Est. tokens | Error |\n")
		fmt.Fprintf(&b, "|---|---:|---:|---:|---:|---:|---|\n")
		for _, q := range backend.Queries {
			fmt.Fprintf(&b, "| %s | %s | %dms | %.2f | %d | %d | %s |\n",
				escapeMarkdownCell(q.Name), q.Status, q.LatencyMillis, q.RelevanceScore, q.ReturnedChars, q.EstimatedTokens, escapeMarkdownCell(q.Error))
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

func elapsedMillis(start, end time.Time) int64 {
	if end.Before(start) {
		return 0
	}
	ms := end.Sub(start).Milliseconds()
	if ms == 0 && !end.Equal(start) {
		return 1
	}
	return ms
}

func benchmarkCapabilitySupported(c BackendCapabilities, capability string) bool {
	switch capability {
	case BenchmarkCapabilitySymbolSearch:
		return c.SymbolSearch
	case BenchmarkCapabilitySemanticSearch:
		return c.SemanticSearch
	case "context_pack":
		return c.ContextPack
	case "graph_trace":
		return c.GraphTrace
	case "edit_refactor":
		return c.EditRefactor
	case "health":
		return c.Health
	default:
		return false
	}
}

func benchmarkReturnedChars(results []BenchmarkResult) int {
	total := 0
	for _, r := range results {
		total += len(r.ID) + len(r.Title) + len(r.Content)
	}
	return total
}

func estimateBenchmarkTokens(results []BenchmarkResult) int {
	total := 0
	for _, r := range results {
		total += estimateBenchmarkTextTokens(r.ID)
		total += estimateBenchmarkTextTokens(r.Title)
		total += estimateBenchmarkTextTokens(r.Content)
	}
	return total
}

func estimateBenchmarkTextTokens(s string) int {
	if s == "" {
		return 0
	}
	ascii := 0
	cjk := 0
	other := 0
	for _, r := range s {
		switch {
		case r <= 0x7f:
			ascii++
		case (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf) || (r >= 0x3040 && r <= 0x30ff) || (r >= 0xac00 && r <= 0xd7af):
			cjk++
		default:
			other++
		}
	}
	tokens := (ascii + 3) / 4
	tokens += cjk
	tokens += (other + 1) / 2
	if tokens == 0 {
		return 1
	}
	return tokens
}

func benchmarkRelevance(results []BenchmarkResult, expected []string, topK int) float64 {
	if len(expected) == 0 {
		return 0
	}
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}
	if topK == 0 {
		return 0
	}
	want := make(map[string]struct{}, len(expected))
	for _, id := range expected {
		want[id] = struct{}{}
	}
	hits := 0
	for i := 0; i < topK; i++ {
		result := results[i]
		for expectedID := range want {
			if benchmarkResultMatchesExpected(result, expectedID) {
				hits++
				delete(want, expectedID)
				break
			}
		}
	}
	return float64(hits) / float64(len(expected))
}

func benchmarkResultMatchesExpected(result BenchmarkResult, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	if result.ID == expected || result.Title == expected {
		return true
	}
	return strings.Contains(result.ID, expected) ||
		strings.Contains(result.Title, expected) ||
		strings.Contains(result.Content, expected)
}

func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func valueOrBenchmark(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func benchmarkSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".kt", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".rb", ".php", ".md":
		return true
	default:
		return false
	}
}
