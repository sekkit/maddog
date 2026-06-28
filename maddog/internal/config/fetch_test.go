package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildModelFetchURLs(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		override string
		want     []string
	}{
		{
			name: "root endpoint keeps legacy models path first",
			base: "https://api.deepseek.com",
			want: []string{"https://api.deepseek.com/models", "https://api.deepseek.com/v1/models"},
		},
		{
			name: "versioned endpoint uses models under version",
			base: "https://api.example.com/v1",
			want: []string{"https://api.example.com/v1/models"},
		},
		{
			name: "non-v1 version keeps v1 fallback",
			base: "https://open.bigmodel.cn/api/coding/paas/v4",
			want: []string{
				"https://open.bigmodel.cn/api/coding/paas/v4/models",
				"https://open.bigmodel.cn/api/coding/paas/v4/v1/models",
			},
		},
		{
			name: "anthropic compatible subpath adds root candidates",
			base: "https://api.deepseek.com/anthropic",
			want: []string{
				"https://api.deepseek.com/anthropic/models",
				"https://api.deepseek.com/anthropic/v1/models",
				"https://api.deepseek.com/models",
				"https://api.deepseek.com/v1/models",
			},
		},
		{
			name:     "override wins",
			base:     "https://api.deepseek.com",
			override: "https://api.deepseek.com/custom/models",
			want:     []string{"https://api.deepseek.com/custom/models"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildModelFetchURLs(tt.base, tt.override)
			if err != nil {
				t.Fatalf("BuildModelFetchURLs: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestProviderFetchModelsFallsBackToV1Models(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-b"}, {"id": "model-a"}},
		})
	}))
	defer srv.Close()

	t.Setenv("FETCH_MODELS_TEST_KEY", "test-key")
	p := ProviderEntry{Name: "test", BaseURL: srv.URL, APIKeyEnv: "FETCH_MODELS_TEST_KEY"}
	got, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(got) != 2 || got[0] != "model-a" || got[1] != "model-b" {
		t.Fatalf("got %v, want [model-a model-b]", got)
	}
}

func TestProviderFetchModelsUsesAnthropicAPIKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "anthropic-key" {
			http.Error(w, "missing anthropic key header", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "authorization header should not be set for api key mode", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "claude-sonnet-4"}},
		})
	}))
	defer srv.Close()

	t.Setenv("FETCH_MODELS_ANTHROPIC_KEY", "anthropic-key")
	p := ProviderEntry{Name: "anthropic", Kind: "anthropic", BaseURL: srv.URL, APIKeyEnv: "FETCH_MODELS_ANTHROPIC_KEY"}
	got, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(got) != 1 || got[0] != "claude-sonnet-4" {
		t.Fatalf("models = %v, want [claude-sonnet-4]", got)
	}
}

func TestProviderFetchModelsUsesBearerTokenEnvWithoutAPIKeyFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bearer-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "gpt-5"}},
		})
	}))
	defer srv.Close()

	t.Setenv("FETCH_MODELS_OLD_API_KEY", "old-api-key")
	t.Setenv("FETCH_MODELS_BEARER_TOKEN", "bearer-token")
	p := ProviderEntry{
		Name:           "openai-official",
		Kind:           "openai",
		BaseURL:        srv.URL,
		APIKeyEnv:      "FETCH_MODELS_OLD_API_KEY",
		AuthType:       "bearer",
		BearerTokenEnv: "FETCH_MODELS_BEARER_TOKEN",
	}
	if got := p.AuthEnvName(); got != "FETCH_MODELS_BEARER_TOKEN" {
		t.Fatalf("AuthEnvName = %q, want bearer token env", got)
	}
	got, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(got) != 1 || got[0] != "gpt-5" {
		t.Fatalf("models = %v, want [gpt-5]", got)
	}
}
