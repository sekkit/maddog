package pxpipe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"

	"maddog/internal/event"
)

// ReadEventSummary scans a pxpipe JSONL event log and returns only safe
// aggregate fields. Missing logs are treated as an empty summary.
func ReadEventSummary(path string) (event.PxpipeSummary, error) {
	if strings.TrimSpace(path) == "" {
		return newPxpipeSummary(), nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newPxpipeSummary(), nil
		}
		return event.PxpipeSummary{}, err
	}
	defer f.Close()
	return SummarizeEvents(f)
}

// SummarizeEvents scans pxpipe JSONL rows. Malformed JSON lines are counted and
// skipped so telemetry can never break a Maddog run.
func SummarizeEvents(r io.Reader) (event.PxpipeSummary, error) {
	summary := newPxpipeSummary()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		row, err := parseEventRow(line)
		if err != nil {
			summary.Malformed++
			continue
		}
		addEventRow(&summary, row)
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

type pxpipeEventRow struct {
	path                string
	model               string
	status              int
	compressed          *bool
	reason              string
	images              int
	baselineTokens      int
	baselineProbeStatus string
	inputTokens         int
	outputTokens        int
	cacheCreateTokens   int
	cacheReadTokens     int
	cachedTokens        int
}

func parseEventRow(line []byte) (pxpipeEventRow, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return pxpipeEventRow{}, err
	}
	return pxpipeEventRow{
		path:                stringField(raw, "path"),
		model:               stringField(raw, "model"),
		status:              intField(raw, "status"),
		compressed:          boolPtrField(raw, "compressed"),
		reason:              stringField(raw, "reason", "compression_status"),
		images:              intField(raw, "image_count", "imageCount"),
		baselineTokens:      intField(raw, "baseline_tokens", "baselineTokens"),
		baselineProbeStatus: stringField(raw, "baseline_probe_status", "baselineProbeStatus"),
		inputTokens:         intField(raw, "input_tokens", "inputTokens"),
		outputTokens:        intField(raw, "output_tokens", "outputTokens"),
		cacheCreateTokens:   intField(raw, "cache_create_tokens", "cacheCreationInputTokens"),
		cacheReadTokens:     intField(raw, "cache_read_tokens", "cacheReadTokens"),
		cachedTokens:        intField(raw, "cached_tokens", "cachedTokens"),
	}, nil
}

func addEventRow(summary *event.PxpipeSummary, row pxpipeEventRow) {
	summary.Requests++
	if row.compressed == nil {
		summary.UnknownCompression++
	} else if *row.compressed {
		summary.Compressed++
	} else {
		summary.PassThrough++
	}
	summary.Images += positive(row.images)
	summary.InputTokens += positive(row.inputTokens)
	summary.OutputTokens += positive(row.outputTokens)
	summary.CacheCreateTokens += positive(row.cacheCreateTokens)
	summary.CacheReadTokens += positive(row.cacheReadTokens)
	summary.CachedTokens += positive(row.cachedTokens)

	switch strings.ToLower(strings.TrimSpace(row.baselineProbeStatus)) {
	case "ok":
		summary.BaselineProbeOK++
		summary.BaselineTokens += positive(row.baselineTokens)
	case "partial":
		summary.BaselineProbePartial++
	case "failed":
		summary.BaselineProbeFailed++
	default:
		summary.BaselineTokens += positive(row.baselineTokens)
	}

	if row.status > 0 {
		summary.Statuses[row.status]++
	}
	increment(summary.Paths, row.path)
	increment(summary.Models, row.model)
	increment(summary.Reasons, row.reason)
}

func newPxpipeSummary() event.PxpipeSummary {
	return event.PxpipeSummary{
		Statuses: map[int]int{},
		Paths:    map[string]int{},
		Models:   map[string]int{},
		Reasons:  map[string]int{},
	}
}

func increment(m map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	m[key]++
}

func positive(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func stringField(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		b, ok := raw[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(b, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func intField(raw map[string]json.RawMessage, keys ...string) int {
	for _, key := range keys {
		b, ok := raw[key]
		if !ok {
			continue
		}
		var v any
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			continue
		}
		switch n := v.(type) {
		case json.Number:
			i, err := strconv.Atoi(n.String())
			if err == nil {
				return i
			}
			f, err := strconv.ParseFloat(n.String(), 64)
			if err == nil {
				return int(f)
			}
		case string:
			i, err := strconv.Atoi(strings.TrimSpace(n))
			if err == nil {
				return i
			}
		}
	}
	return 0
}

func boolPtrField(raw map[string]json.RawMessage, key string) *bool {
	b, ok := raw[key]
	if !ok {
		return nil
	}
	var v bool
	if err := json.Unmarshal(b, &v); err == nil {
		return &v
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true":
			v = true
			return &v
		case "false":
			v = false
			return &v
		}
	}
	return nil
}
