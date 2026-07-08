package pxpipe

import (
	"fmt"
	"strings"
	"testing"
)

func TestSummarizeEventsAggregatesSafeFields(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"ts":"2026-07-08T01:00:00Z","method":"POST","path":"/v1/messages","model":"claude-fable-5","status":200,"compressed":true,"reason":"applied","image_count":3,"baseline_tokens":1000,"baseline_probe_status":"ok","input_tokens":300,"output_tokens":40,"cache_create_tokens":20,"cache_read_tokens":30,"req_body_sample_b64":"secret prompt sk-live","error_body":"secret prompt sk-live"}`,
		`{"ts":"2026-07-08T01:00:01Z","method":"POST","path":"/v1/responses","model":"gpt-5.5","status":200,"compressed":false,"reason":"unsupported_model","baseline_tokens":500,"baseline_probe_status":"partial","input_tokens":150,"cached_tokens":15}`,
		`not-json`,
		`{"ts":"2026-07-08T01:00:02Z","method":"POST","path":"/v1/messages","model":"claude-fable-5","status":502,"compressed":false,"reason":"upstream_error","baseline_probe_status":"failed","req_body_sample_path":"/tmp/secret.gz"}`,
	}, "\n")

	summary, err := SummarizeEvents(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("SummarizeEvents: %v", err)
	}

	if summary.Requests != 3 || summary.Malformed != 1 {
		t.Fatalf("requests/malformed = %d/%d, want 3/1", summary.Requests, summary.Malformed)
	}
	if summary.Compressed != 1 || summary.PassThrough != 2 {
		t.Fatalf("compressed/pass-through = %d/%d, want 1/2", summary.Compressed, summary.PassThrough)
	}
	if summary.Images != 3 {
		t.Fatalf("images = %d, want 3", summary.Images)
	}
	if summary.BaselineTokens != 1000 || summary.BaselineProbeOK != 1 || summary.BaselineProbePartial != 1 || summary.BaselineProbeFailed != 1 {
		t.Fatalf("baseline summary = tokens:%d ok:%d partial:%d failed:%d, want 1000/1/1/1",
			summary.BaselineTokens, summary.BaselineProbeOK, summary.BaselineProbePartial, summary.BaselineProbeFailed)
	}
	if summary.InputTokens != 450 || summary.OutputTokens != 40 || summary.CacheCreateTokens != 20 || summary.CacheReadTokens != 30 || summary.CachedTokens != 15 {
		t.Fatalf("token summary = in:%d out:%d create:%d read:%d cached:%d, want 450/40/20/30/15",
			summary.InputTokens, summary.OutputTokens, summary.CacheCreateTokens, summary.CacheReadTokens, summary.CachedTokens)
	}
	if summary.Models["claude-fable-5"] != 2 || summary.Models["gpt-5.5"] != 1 {
		t.Fatalf("models = %+v", summary.Models)
	}
	if summary.Paths["/v1/messages"] != 2 || summary.Paths["/v1/responses"] != 1 {
		t.Fatalf("paths = %+v", summary.Paths)
	}
	if summary.Reasons["applied"] != 1 || summary.Reasons["unsupported_model"] != 1 || summary.Reasons["upstream_error"] != 1 {
		t.Fatalf("reasons = %+v", summary.Reasons)
	}

	rendered := fmt.Sprintf("%+v", summary)
	if strings.Contains(rendered, "secret prompt") || strings.Contains(rendered, "sk-live") || strings.Contains(rendered, "secret.gz") {
		t.Fatalf("summary leaked raw body sample fields: %s", rendered)
	}
}
