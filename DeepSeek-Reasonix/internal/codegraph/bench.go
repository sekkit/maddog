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
	"unicode"
	"unicode/utf8"
)

type BenchmarkQueryKind string

const (
	BenchmarkQuerySymbol   BenchmarkQueryKind = "symbol"
	BenchmarkQuerySemantic BenchmarkQueryKind = "semantic"
)

type BenchmarkBackend interface {
	ID() string
	Name() string
	Capabilities() []BackendCapability
	Health() BackendHealth
	BuildIndex(context.Context, string) error
	UpdateIndex(context.Context, BenchmarkUpdate) error
	Query(context.Context, BenchmarkQuery) ([]BenchmarkSearchResult, error)
}

type BenchmarkOptions struct {
	Root     string
	Backends []BenchmarkBackend
	Queries  []BenchmarkQuery
	Now      time.Time
	Update   BenchmarkUpdate
}

type BenchmarkUpdate struct {
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
}

type BenchmarkQuery struct {
	ID    string             `json:"id"`
	Kind  BenchmarkQueryKind `json:"kind"`
	Query string             `json:"query"`
	TopK  int                `json:"top_k"`
	Want  []string           `json:"want,omitempty"`
}

type BenchmarkSearchResult struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type BenchmarkReport struct {
	GeneratedAt string                   `json:"generated_at"`
	Root        string                   `json:"root,omitempty"`
	Backends    []BackendBenchmarkReport `json:"backends"`
}

type BackendBenchmarkReport struct {
	BackendID           string                `json:"backend_id"`
	BackendName         string                `json:"backend_name"`
	Health              BackendHealth         `json:"health"`
	Capabilities        []string              `json:"capabilities,omitempty"`
	IndexBuildMS        int64                 `json:"index_build_ms"`
	IncrementalUpdateMS int64                 `json:"incremental_update_ms"`
	QueryLatencyMS      int64                 `json:"query_latency_ms"`
	TopKRelevance       float64               `json:"top_k_relevance"`
	CitationPrecision   float64               `json:"citation_precision"`
	TokenCharsReturned  int                   `json:"token_chars_returned"`
	ToolFailures        int                   `json:"tool_failures"`
	Unsupported         int                   `json:"unsupported"`
	Cases               []BenchmarkCaseReport `json:"cases"`
}

type BenchmarkCaseReport struct {
	ID                 string                  `json:"id"`
	Kind               BenchmarkQueryKind      `json:"kind"`
	Query              string                  `json:"query"`
	Supported          bool                    `json:"supported"`
	Unsupported        bool                    `json:"unsupported,omitempty"`
	LatencyMS          int64                   `json:"latency_ms"`
	TopKRelevance      float64                 `json:"top_k_relevance"`
	CitationPrecision  float64                 `json:"citation_precision"`
	TokenCharsReturned int                     `json:"token_chars_returned"`
	Failure            string                  `json:"failure,omitempty"`
	Results            []BenchmarkSearchResult `json:"results,omitempty"`
	ExplorationTrace   []string                `json:"exploration_trace,omitempty"`
}

type BenchmarkDoctorSummary struct {
	Path        string                          `json:"path"`
	GeneratedAt string                          `json:"generated_at,omitempty"`
	Backends    []BenchmarkDoctorBackendSummary `json:"backends,omitempty"`
}

type BenchmarkDoctorBackendSummary struct {
	BackendID          string        `json:"backend_id"`
	Health             BackendHealth `json:"health"`
	TopKRelevance      float64       `json:"top_k_relevance"`
	CitationPrecision  float64       `json:"citation_precision"`
	ToolFailures       int           `json:"tool_failures"`
	Unsupported        int           `json:"unsupported"`
	TokenCharsReturned int           `json:"token_chars_returned"`
}

func RunBenchmark(ctx context.Context, opts BenchmarkOptions) (BenchmarkReport, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = "."
	}
	queries := opts.Queries
	if len(queries) == 0 {
		queries = DefaultBenchmarkQueries()
	}
	update := opts.Update
	if strings.TrimSpace(update.Path) == "" {
		update = BenchmarkUpdate{Path: ".maddog/codeintelbench-incremental.txt", Content: "maddog code intelligence benchmark incremental update\n"}
	}
	report := BenchmarkReport{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Root:        root,
		Backends:    make([]BackendBenchmarkReport, 0, len(opts.Backends)),
	}
	for _, backend := range opts.Backends {
		if backend == nil {
			continue
		}
		report.Backends = append(report.Backends, runBackendBenchmark(ctx, backend, root, queries, update))
	}
	return report, nil
}

func runBackendBenchmark(ctx context.Context, backend BenchmarkBackend, root string, queries []BenchmarkQuery, update BenchmarkUpdate) BackendBenchmarkReport {
	out := BackendBenchmarkReport{
		BackendID:      strings.TrimSpace(backend.ID()),
		BackendName:    strings.TrimSpace(backend.Name()),
		Health:         backend.Health(),
		Capabilities:   SortedBackendCapabilities(backend.Capabilities()),
		IndexBuildMS:   -1,
		QueryLatencyMS: -1,
	}
	if out.BackendName == "" {
		out.BackendName = out.BackendID
	}
	start := time.Now()
	if err := backend.BuildIndex(ctx, root); err != nil {
		out.ToolFailures++
		out.Cases = append(out.Cases, BenchmarkCaseReport{
			ID:        "index-build",
			Kind:      BenchmarkQuerySymbol,
			Query:     "build index",
			Supported: true,
			Failure:   err.Error(),
		})
	} else {
		out.IndexBuildMS = durationMillisSince(start)
	}

	start = time.Now()
	if err := backend.UpdateIndex(ctx, update); err != nil {
		out.ToolFailures++
		out.Cases = append(out.Cases, BenchmarkCaseReport{
			ID:        "incremental-update",
			Kind:      BenchmarkQuerySymbol,
			Query:     update.Path,
			Supported: true,
			Failure:   err.Error(),
		})
	} else {
		out.IncrementalUpdateMS = durationMillisSince(start)
	}

	var relevanceSum float64
	var relevanceCount int
	var citationSum float64
	var citationCount int
	var latencySum int64
	for _, query := range queries {
		if strings.TrimSpace(query.ID) == "" {
			query.ID = sanitizeQueryID(query.Query)
		}
		if query.Kind == "" {
			query.Kind = BenchmarkQuerySymbol
		}
		if query.TopK <= 0 {
			query.TopK = 5
		}
		c := BenchmarkCaseReport{
			ID:        query.ID,
			Kind:      query.Kind,
			Query:     query.Query,
			Supported: true,
		}
		if !benchmarkSupports(backend.Capabilities(), query.Kind) {
			c.Supported = false
			c.Unsupported = true
			out.Unsupported++
			out.Cases = append(out.Cases, c)
			continue
		}
		start := time.Now()
		results, err := backend.Query(ctx, query)
		c.LatencyMS = durationMillisSince(start)
		latencySum += c.LatencyMS
		if err != nil {
			c.Failure = err.Error()
			out.ToolFailures++
			out.Cases = append(out.Cases, c)
			continue
		}
		if len(results) > query.TopK {
			results = results[:query.TopK]
		}
		c.Results = append([]BenchmarkSearchResult(nil), results...)
		c.TokenCharsReturned = tokenChars(results)
		c.TopKRelevance = topKRelevance(results, query.Want)
		c.CitationPrecision = citationPrecision(results)
		c.ExplorationTrace = compactExplorationTrace(results)
		out.TokenCharsReturned += c.TokenCharsReturned
		relevanceSum += c.TopKRelevance
		relevanceCount++
		citationSum += c.CitationPrecision
		citationCount++
		out.Cases = append(out.Cases, c)
	}
	if len(queries) > 0 {
		out.QueryLatencyMS = latencySum
	}
	if relevanceCount > 0 {
		out.TopKRelevance = relevanceSum / float64(relevanceCount)
	}
	if citationCount > 0 {
		out.CitationPrecision = citationSum / float64(citationCount)
	}
	return out
}

func DefaultBenchmarkQueries() []BenchmarkQuery {
	return []BenchmarkQuery{
		{
			ID:    "loop-template-symbol",
			Kind:  BenchmarkQuerySymbol,
			Query: "LoopTemplateV1",
			TopK:  5,
			Want:  []string{"LoopTemplateV1"},
		},
		{
			ID:    "readiness-symbol",
			Kind:  BenchmarkQuerySymbol,
			Query: "ReadinessResult",
			TopK:  5,
			Want:  []string{"ReadinessResult"},
		},
		{
			ID:    "provider-auth-semantic",
			Kind:  BenchmarkQuerySemantic,
			Query: "provider auth credential status",
			TopK:  5,
			Want:  []string{"Provider", "Auth"},
		},
	}
}

func NewBuiltInBenchmarkBackend() BenchmarkBackend {
	return &localFileBenchmarkBackend{
		id:   BuiltInBackendID,
		name: "CodeGraph",
		capabilities: []BackendCapability{
			BackendCapabilitySymbolSearch,
			BackendCapabilityContextPack,
			BackendCapabilityGraphTrace,
			BackendCapabilityHealth,
		},
	}
}

func NewMockBenchmarkBackend() BenchmarkBackend {
	return &mockBenchmarkBackend{}
}

func WriteBenchmarkReportJSON(path string, report BenchmarkReport) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("codegraph benchmark: empty JSON report path")
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func WriteBenchmarkReportMarkdown(path string, report BenchmarkReport) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("codegraph benchmark: empty markdown report path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(report.Markdown()), 0o644)
}

func WriteLatestBenchmarkReport(cacheDir string, report BenchmarkReport) (string, error) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("codegraph benchmark: cache dir unavailable")
	}
	dir := filepath.Join(cacheDir, "codeintelbench")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "latest.json")
	if err := WriteBenchmarkReportJSON(path, report); err != nil {
		return "", err
	}
	return path, nil
}

func ReadLatestBenchmarkSummary(cacheDir string) (BenchmarkDoctorSummary, bool) {
	path := filepath.Join(strings.TrimSpace(cacheDir), "codeintelbench", "latest.json")
	if strings.TrimSpace(cacheDir) == "" {
		return BenchmarkDoctorSummary{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return BenchmarkDoctorSummary{}, false
	}
	var report BenchmarkReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return BenchmarkDoctorSummary{}, false
	}
	summary := BenchmarkDoctorSummary{
		Path:        path,
		GeneratedAt: report.GeneratedAt,
		Backends:    make([]BenchmarkDoctorBackendSummary, 0, len(report.Backends)),
	}
	for _, b := range report.Backends {
		summary.Backends = append(summary.Backends, BenchmarkDoctorBackendSummary{
			BackendID:          b.BackendID,
			Health:             b.Health,
			TopKRelevance:      b.TopKRelevance,
			CitationPrecision:  b.CitationPrecision,
			ToolFailures:       b.ToolFailures,
			Unsupported:        b.Unsupported,
			TokenCharsReturned: b.TokenCharsReturned,
		})
	}
	return summary, true
}

func (r BenchmarkReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Code intelligence benchmark\n\n")
	if r.GeneratedAt != "" {
		fmt.Fprintf(&b, "**Generated:** %s\n\n", r.GeneratedAt)
	}
	fmt.Fprintf(&b, "| Backend | Health | Index | Update | Query | Relevance | Citation | Chars | Failures | Unsupported |\n")
	fmt.Fprintf(&b, "|---------|--------|------:|-------:|------:|----------:|---------:|------:|---------:|------------:|\n")
	for _, item := range r.Backends {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s | %d | %d | %d |\n",
			item.BackendID,
			item.Health,
			millisLabel(item.IndexBuildMS),
			millisLabel(item.IncrementalUpdateMS),
			millisLabel(item.QueryLatencyMS),
			percentLabel(item.TopKRelevance),
			percentLabel(item.CitationPrecision),
			item.TokenCharsReturned,
			item.ToolFailures,
			item.Unsupported,
		)
	}
	if len(r.Backends) == 0 {
		fmt.Fprintf(&b, "| none | unavailable | n/a | n/a | n/a | n/a | n/a | 0 | 0 | 0 |\n")
	}
	return b.String()
}

type localFileBenchmarkBackend struct {
	id           string
	name         string
	capabilities []BackendCapability
	files        []benchmarkFile
}

type benchmarkFile struct {
	path  string
	lines []string
}

func (b *localFileBenchmarkBackend) ID() string { return b.id }

func (b *localFileBenchmarkBackend) Name() string { return b.name }

func (b *localFileBenchmarkBackend) Capabilities() []BackendCapability {
	return append([]BackendCapability(nil), b.capabilities...)
}

func (b *localFileBenchmarkBackend) Health() BackendHealth { return BackendHealthAvailable }

func (b *localFileBenchmarkBackend) BuildIndex(ctx context.Context, root string) error {
	var files []benchmarkFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		name := d.Name()
		if d.IsDir() {
			if shouldSkipBenchDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipBenchFile(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 512*1024 {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(raw) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		files = append(files, benchmarkFile{path: filepath.ToSlash(rel), lines: strings.Split(string(raw), "\n")})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	b.files = files
	return nil
}

func (b *localFileBenchmarkBackend) UpdateIndex(_ context.Context, update BenchmarkUpdate) error {
	if strings.TrimSpace(update.Path) == "" {
		return nil
	}
	b.files = append(b.files, benchmarkFile{path: filepath.ToSlash(update.Path), lines: strings.Split(update.Content, "\n")})
	return nil
}

func (b *localFileBenchmarkBackend) Query(ctx context.Context, query BenchmarkQuery) ([]BenchmarkSearchResult, error) {
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	if needle == "" {
		return nil, nil
	}
	topK := query.TopK
	if topK <= 0 {
		topK = 5
	}
	var out []BenchmarkSearchResult
	for _, file := range b.files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		pathHit := strings.Contains(strings.ToLower(file.path), needle)
		for i, line := range file.lines {
			lower := strings.ToLower(line)
			if pathHit || strings.Contains(lower, needle) || tokenOverlapScore(needle, lower) > 0 {
				out = append(out, BenchmarkSearchResult{
					Path:    file.path,
					Line:    i + 1,
					Symbol:  extractSymbol(line),
					Snippet: strings.TrimSpace(line),
				})
				if len(out) >= topK {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

type mockBenchmarkBackend struct{}

func (b *mockBenchmarkBackend) ID() string { return "mock" }

func (b *mockBenchmarkBackend) Name() string { return "Mock code intelligence" }

func (b *mockBenchmarkBackend) Capabilities() []BackendCapability {
	return []BackendCapability{BackendCapabilitySymbolSearch, BackendCapabilitySemanticSearch, BackendCapabilityContextPack, BackendCapabilityHealth}
}

func (b *mockBenchmarkBackend) Health() BackendHealth { return BackendHealthAvailable }

func (b *mockBenchmarkBackend) BuildIndex(context.Context, string) error { return nil }

func (b *mockBenchmarkBackend) UpdateIndex(context.Context, BenchmarkUpdate) error { return nil }

func (b *mockBenchmarkBackend) Query(_ context.Context, query BenchmarkQuery) ([]BenchmarkSearchResult, error) {
	want := query.Want
	if len(want) == 0 {
		want = []string{query.Query}
	}
	results := make([]BenchmarkSearchResult, 0, len(want))
	for i, item := range want {
		results = append(results, BenchmarkSearchResult{
			Path:    fmt.Sprintf("fixtures/%s.go", sanitizeQueryID(query.ID)),
			Line:    i + 1,
			Symbol:  item,
			Snippet: item + " benchmark fixture",
		})
	}
	return results, nil
}

func benchmarkSupports(caps []BackendCapability, kind BenchmarkQueryKind) bool {
	switch kind {
	case BenchmarkQuerySemantic:
		return hasCapability(caps, BackendCapabilitySemanticSearch)
	default:
		return hasCapability(caps, BackendCapabilitySymbolSearch) || hasCapability(caps, BackendCapabilityContextPack)
	}
}

func hasCapability(items []BackendCapability, want BackendCapability) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func topKRelevance(results []BenchmarkSearchResult, want []string) float64 {
	if len(want) == 0 {
		if len(results) > 0 {
			return 1
		}
		return 0
	}
	joined := strings.ToLower(resultsText(results))
	matched := 0
	for _, item := range want {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && strings.Contains(joined, item) {
			matched++
		}
	}
	return float64(matched) / float64(len(want))
}

func tokenChars(results []BenchmarkSearchResult) int {
	return len(resultsText(results))
}

func citationPrecision(results []BenchmarkSearchResult) float64 {
	if len(results) == 0 {
		return 0
	}
	var cited int
	for _, result := range results {
		if strings.TrimSpace(result.Path) != "" && result.Line > 0 {
			cited++
		}
	}
	return float64(cited) / float64(len(results))
}

func compactExplorationTrace(results []BenchmarkSearchResult) []string {
	out := make([]string, 0, len(results))
	for _, result := range results {
		path := strings.TrimSpace(result.Path)
		if path == "" {
			continue
		}
		if result.Line > 0 {
			out = append(out, fmt.Sprintf("%s:%d", path, result.Line))
			continue
		}
		out = append(out, path)
	}
	return out
}

func resultsText(results []BenchmarkSearchResult) string {
	var b strings.Builder
	for _, r := range results {
		b.WriteString(r.Path)
		b.WriteByte('\n')
		b.WriteString(r.Symbol)
		b.WriteByte('\n')
		b.WriteString(r.Snippet)
		b.WriteByte('\n')
	}
	return b.String()
}

func durationMillisSince(start time.Time) int64 {
	if start.IsZero() {
		return -1
	}
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func millisLabel(ms int64) string {
	if ms < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%dms", ms)
}

func percentLabel(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}

func sanitizeQueryID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "query"
	}
	return out
}

func shouldSkipBenchDir(name string) bool {
	switch name {
	case ".git", ".codegraph", "node_modules", "dist", "vendor", ".maddog":
		return true
	default:
		return false
	}
}

func shouldSkipBenchFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".md", ".toml", ".yaml", ".yml", ".css", ".html", ".txt":
		return false
	default:
		return true
	}
}

func extractSymbol(line string) string {
	fields := strings.FieldsFunc(line, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
	for i, field := range fields {
		switch field {
		case "func", "type", "const", "var", "interface", "struct":
			if i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	for _, field := range fields {
		if field != "" {
			return field
		}
	}
	return ""
}

func tokenOverlapScore(query, text string) int {
	score := 0
	for _, token := range strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(token) >= 3 && strings.Contains(text, token) {
			score++
		}
	}
	return score
}
