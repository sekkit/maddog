package config

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"maddog/internal/provider"
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

	p := ProviderEntry{Name: "test", BaseURL: srv.URL, APIKeyEnv: "FETCH_MODELS_TEST_KEY", resolvedAPIKey: "test-key"}
	got, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(got) != 2 || got[0] != "model-a" || got[1] != "model-b" {
		t.Fatalf("got %v, want [model-a model-b]", got)
	}
}

func TestProviderFetchModelsContinuesAfterRootAuthFailure(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/models":
			http.Error(w, `{"error":"wrong endpoint"}`, http.StatusUnauthorized)
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer test-key" {
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "model-a"}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ProviderEntry{Name: "test", BaseURL: srv.URL, APIKeyEnv: "FETCH_MODELS_TEST_KEY", resolvedAPIKey: "test-key"}
	got, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(got) != 1 || got[0] != "model-a" {
		t.Fatalf("got %v, want [model-a]", got)
	}
	if len(paths) != 2 || paths[0] != "/models" || paths[1] != "/v1/models" {
		t.Fatalf("paths = %v, want [/models /v1/models]", paths)
	}
}

func TestProviderFetchModelsReturnsProviderAuthErrorMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid token"}}`, http.StatusForbidden)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_ACCESS_TOKEN", "bad-token")
	p := ProviderEntry{
		Name:         "official-openai",
		BaseURL:      srv.URL,
		AuthType:     provider.AuthTypeBearer,
		AuthTokenEnv: "OPENAI_ACCESS_TOKEN",
	}
	_, err := p.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
	var authErr *provider.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %[1]v, want provider.AuthError", err)
	}
	if authErr.Provider != "official-openai" || authErr.KeyEnv != "OPENAI_ACCESS_TOKEN" || authErr.Status != http.StatusForbidden {
		t.Fatalf("auth error = %+v", authErr)
	}
}

func TestProviderFetchModelsExchangesWorkloadIdentity(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "probe-access-token",
				"token_type":   "bearer",
			})
		case "/models":
			gotAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "gpt-5.5"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("OPENAI_IDENTITY_TOKEN", "external-oidc-jwt")
	p := ProviderEntry{
		Name:               "official-openai",
		BaseURL:            srv.URL,
		AuthType:           provider.AuthTypeWorkloadIdentity,
		IdentityEnv:        "OPENAI_IDENTITY_TOKEN",
		IdentityProviderID: "wip_openai",
		ServiceAcctID:      "svc_openai",
		TokenURL:           srv.URL + "/oauth/token",
	}
	models, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if gotAuth != "Bearer probe-access-token" {
		t.Fatalf("Authorization = %q, want exchanged token", gotAuth)
	}
	if len(models) != 1 || models[0] != "gpt-5.5" {
		t.Fatalf("models = %v, want [gpt-5.5]", models)
	}
}
