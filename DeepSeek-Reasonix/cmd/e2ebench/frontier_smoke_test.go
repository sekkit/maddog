package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunFrontierSmokeCoversProviderScorerCostAndAdvisor(t *testing.T) {
	t.Setenv("ICODEEASY_API_KEY", "icodeeasy-live-key")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-live-key")

	var openAIAuth string
	var sawScorer bool
	openAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected OpenAI path %s", r.URL.Path)
		}
		openAIAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode OpenAI body: %v", err)
		}
		if got := body["reasoning"].(map[string]any)["effort"]; got != "high" {
			t.Fatalf("reasoning effort = %v, want high", got)
		}
		if strings.Contains(strings.ToLower(toString(body)), "score the replayed outcome") {
			sawScorer = true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if sawScorer {
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"0.91 strong replay\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":7,\"output_tokens\":5,\"total_tokens\":12}}}\n\n")
		} else {
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"total_tokens\":7}}}\n\n")
		}
	}))
	defer openAIServer.Close()

	var anthropicKey, anthropicBeta string
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected Anthropic path %s", r.URL.Path)
		}
		anthropicKey = r.Header.Get("x-api-key")
		anthropicBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"advisor ok\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer anthropicServer.Close()

	result := runFrontierSmoke(frontierSmokeConfig{
		OpenAIBaseURL:    openAIServer.URL,
		OpenAIModel:      "gpt-frontier-test",
		AnthropicBaseURL: anthropicServer.URL,
		AnthropicModel:   "claude-frontier-test",
		TimeoutSec:       5,
	})

	if !result.Passed {
		t.Fatalf("frontier smoke failed: %+v", result)
	}
	if openAIAuth != "Bearer icodeeasy-live-key" {
		t.Fatalf("OpenAI Authorization = %q", openAIAuth)
	}
	if !sawScorer {
		t.Fatal("expected C2 scorer prompt to reach frontier provider")
	}
	if result.Costwrap.OutputTokens <= 0 {
		t.Fatalf("costwrap output tokens = %d", result.Costwrap.OutputTokens)
	}
	if anthropicKey != "anthropic-live-key" {
		t.Fatalf("Anthropic x-api-key = %q", anthropicKey)
	}
	if !strings.Contains(anthropicBeta, "advisor-tool-2026-03-01") {
		t.Fatalf("Anthropic beta header = %q", anthropicBeta)
	}
	if result.Scorer.Score < 0.9 {
		t.Fatalf("scorer result = %+v", result.Scorer)
	}
}

func TestFrontierAndOfficialAuthSmokeUseModeSpecificOpenAIBaseDefaults(t *testing.T) {
	frontier := normalizeFrontierSmokeConfig(frontierSmokeConfig{})
	if frontier.OpenAIBaseURL != defaultIcodeEasyURL {
		t.Fatalf("frontier OpenAI base URL = %q, want %q", frontier.OpenAIBaseURL, defaultIcodeEasyURL)
	}

	official := normalizeOfficialAuthSmokeConfig(officialAuthSmokeConfig{})
	if official.OpenAIBaseURL != defaultOpenAIURL {
		t.Fatalf("official auth OpenAI base URL = %q, want %q", official.OpenAIBaseURL, defaultOpenAIURL)
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, part := range x {
			b.WriteString(toString(part))
		}
		return b.String()
	case map[string]any:
		if s, ok := x["text"].(string); ok {
			return s
		}
		if s, ok := x["content"].(string); ok {
			return s
		}
		var b strings.Builder
		for _, part := range x {
			b.WriteString(toString(part))
		}
		return b.String()
	}
	return ""
}
