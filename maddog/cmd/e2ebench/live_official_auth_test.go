package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunOfficialAuthSmokeUsesWorkloadIdentityForOpenAIAndAnthropic(t *testing.T) {
	t.Setenv("OPENAI_IDENTITY_TOKEN", "openai-identity-jwt")
	t.Setenv("OPENAI_IDENTITY_PROVIDER_ID", "wip_live")
	t.Setenv("OPENAI_SERVICE_ACCOUNT_ID", "svc_live")
	t.Setenv("OPENAI_SUBJECT_TOKEN_TYPE", "urn:ietf:params:oauth:token-type:id_token")
	t.Setenv("ANTHROPIC_IDENTITY_TOKEN", "anthropic-identity-jwt")
	t.Setenv("ANTHROPIC_FEDERATION_RULE_ID", "fdrl_live")
	t.Setenv("ANTHROPIC_ORGANIZATION_ID", "org_live")
	t.Setenv("ANTHROPIC_SERVICE_ACCOUNT_ID", "svac_live")
	t.Setenv("ANTHROPIC_WORKSPACE_ID", "wsp_live")

	var gotOpenAIAuth string
	var sawOpenAIExchange bool
	var sawAnthropicExchange bool
	var gotAnthropicAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			sawOpenAIExchange = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode OpenAI token exchange: %v", err)
			}
			if body["grant_type"] != "urn:ietf:params:oauth:grant-type:token-exchange" ||
				body["subject_token_type"] != "urn:ietf:params:oauth:token-type:id_token" ||
				body["subject_token"] != "openai-identity-jwt" ||
				body["identity_provider_id"] != "wip_live" ||
				body["service_account_id"] != "svc_live" {
				t.Fatalf("OpenAI exchange body = %+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"openai-live-token","token_type":"bearer","expires_in":3600}`)
		case "/chat/completions":
			gotOpenAIAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case "/v1/oauth/token":
			sawAnthropicExchange = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode token exchange: %v", err)
			}
			if body["grant_type"] != "urn:ietf:params:oauth:grant-type:jwt-bearer" ||
				body["assertion"] != "anthropic-identity-jwt" ||
				body["federation_rule_id"] != "fdrl_live" ||
				body["organization_id"] != "org_live" ||
				body["service_account_id"] != "svac_live" ||
				body["workspace_id"] != "wsp_live" {
				t.Fatalf("exchange body = %+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"anthropic-live-token","token_type":"bearer","expires_in":3600}`)
		case "/v1/messages":
			gotAnthropicAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
			_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
			_, _ = io.WriteString(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
			_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("OPENAI_WORKLOAD_TOKEN_URL", srv.URL+"/oauth/token")

	result := runOfficialAuthSmoke(officialAuthSmokeConfig{
		OpenAIBaseURL:    srv.URL,
		OpenAIModel:      "gpt-test",
		AnthropicBaseURL: srv.URL,
		AnthropicModel:   "claude-test",
		TimeoutSec:       5,
	})

	if !result.Passed {
		t.Fatalf("official auth smoke failed: %+v", result)
	}
	if !sawOpenAIExchange {
		t.Fatal("expected OpenAI workload identity exchange")
	}
	if gotOpenAIAuth != "Bearer openai-live-token" {
		t.Fatalf("OpenAI Authorization = %q", gotOpenAIAuth)
	}
	if !sawAnthropicExchange {
		t.Fatal("expected Anthropic workload identity exchange")
	}
	if gotAnthropicAuth != "Bearer anthropic-live-token" {
		t.Fatalf("Anthropic Authorization = %q", gotAnthropicAuth)
	}
	if !strings.Contains(result.Note, "openai=ok") || !strings.Contains(result.Note, "anthropic=ok") {
		t.Fatalf("result note should summarize provider success: %q", result.Note)
	}
}
