package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestFetchModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "model-b", "object": "model"},
				{"id": "model-a", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0] != "model-a" || models[1] != "model-b" {
		t.Errorf("want sorted [model-a model-b], got %v", models)
	}
}

func TestFetchModelsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchModels(context.Background(), srv.URL, "bad-key")
	if err == nil {
		t.Fatal("expected error for bad key")
	}
}

func TestFetchModelsEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": nil})
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("want empty list, got %v", models)
	}
}

func TestFetchModelsWithAuthUsesProviderDefaultAPIKeyHeader(t *testing.T) {
	var gotAuthorization, gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		if gotAPIKey != "provider-key" || gotAuthorization != "" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "model-a"}}})
	}))
	defer srv.Close()

	models, err := FetchModelsWithAuth(context.Background(), srv.URL, provider.AuthConfig{
		Type:  provider.AuthTypeAPIKey,
		Token: "provider-key",
	}, "x-api-key")
	if err != nil {
		t.Fatalf("FetchModelsWithAuth: %v", err)
	}
	if len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("models = %v, want [model-a]", models)
	}
}

func TestFetchModelsWithAuthUsesBearerAuthorizationOnly(t *testing.T) {
	var gotAuthorization, gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		if gotAuthorization != "Bearer official-token" || gotAPIKey != "" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "gpt-5"}}})
	}))
	defer srv.Close()

	models, err := FetchModelsWithAuth(context.Background(), srv.URL, provider.AuthConfig{
		Type:     provider.AuthTypeBearer,
		Token:    "official-token",
		TokenEnv: "OPENAI_ACCESS_TOKEN",
	}, "x-api-key")
	if err != nil {
		t.Fatalf("FetchModelsWithAuth: %v", err)
	}
	if len(models) != 1 || models[0] != "gpt-5" {
		t.Fatalf("models = %v, want [gpt-5]", models)
	}
}

func TestClassifyModelFetchErrorAndRedactBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Authorization: Bearer sk-fetch-secret"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchModelsWithAuth(context.Background(), srv.URL, provider.AuthConfig{Type: provider.AuthTypeBearer, Token: "bad"}, "Authorization")
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if got := ClassifyModelFetchError(err); got != ModelFetchAuthFailure {
		t.Fatalf("ClassifyModelFetchError = %q, want %q", got, ModelFetchAuthFailure)
	}
	if strings.Contains(err.Error(), "sk-fetch-secret") {
		t.Fatalf("fetch error leaked secret body: %v", err)
	}
}

func TestClassifyModelFetchTimeoutAsProviderUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "late-model"}}})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	_, err := FetchModelsWithAuth(ctx, srv.URL, provider.AuthConfig{Type: provider.AuthTypeAPIKey, Token: "key"}, "Authorization")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if got := ClassifyModelFetchError(err); got != ModelFetchProviderUnavailable {
		t.Fatalf("ClassifyModelFetchError = %q, want %q", got, ModelFetchProviderUnavailable)
	}
}
