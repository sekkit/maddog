package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"maddog/internal/provider"
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

func TestFetchModelsWithBearerAuthConfig(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "gpt-5"}},
		})
	}))
	defer srv.Close()

	models, err := FetchModelsWithAuth(context.Background(), srv.URL, "official-openai", provider.AuthConfig{
		Type:     provider.AuthTypeBearer,
		Token:    "official-access-token",
		TokenEnv: "OPENAI_ACCESS_TOKEN",
	})
	if err != nil {
		t.Fatalf("FetchModelsWithAuth: %v", err)
	}
	if gotAuth != "Bearer official-access-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if len(models) != 1 || models[0] != "gpt-5" {
		t.Fatalf("models = %v, want [gpt-5]", models)
	}
}

func TestFetchModelsWithAuthErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid token"}}`, http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := FetchModelsWithAuth(context.Background(), srv.URL, "official-openai", provider.AuthConfig{
		Type:     provider.AuthTypeBearer,
		Token:    "bad-token",
		TokenEnv: "OPENAI_ACCESS_TOKEN",
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
	var authErr *provider.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %[1]v, want provider.AuthError", err)
	}
	if authErr.Provider != "official-openai" || authErr.KeyEnv != "OPENAI_ACCESS_TOKEN" || authErr.Status != http.StatusForbidden || !authErr.HasKey {
		t.Fatalf("auth error = %+v", authErr)
	}
}

func TestFetchModelsWithWorkloadIdentityExchangesToken(t *testing.T) {
	var gotTokenBody map[string]string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := json.NewDecoder(r.Body).Decode(&gotTokenBody); err != nil {
				t.Fatalf("decode token request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "exchanged-access-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})
		case "/models":
			gotAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "gpt-5.5"}},
			})
		default:
			body, _ := io.ReadAll(r.Body)
			t.Fatalf("unexpected request %s body=%s", r.URL.Path, string(body))
		}
	}))
	defer srv.Close()

	models, err := FetchModelsWithAuth(context.Background(), srv.URL, "official-openai", provider.AuthConfig{
		Type:          provider.AuthTypeWorkloadIdentity,
		IdentityToken: "external-oidc-jwt",
		Extra: map[string]string{
			"identity_provider_id": "wip_openai",
			"service_account_id":   "svc_openai",
			"subject_token_type":   "urn:ietf:params:oauth:token-type:id_token",
			"token_url":            srv.URL + "/oauth/token",
		},
	})
	if err != nil {
		t.Fatalf("FetchModelsWithAuth: %v", err)
	}
	if gotTokenBody["subject_token"] != "external-oidc-jwt" || gotTokenBody["identity_provider_id"] != "wip_openai" {
		t.Fatalf("token request = %+v", gotTokenBody)
	}
	if gotAuth != "Bearer exchanged-access-token" {
		t.Fatalf("Authorization = %q, want exchanged token", gotAuth)
	}
	if len(models) != 1 || models[0] != "gpt-5.5" {
		t.Fatalf("models = %v, want [gpt-5.5]", models)
	}
}

func TestFetchModelsWithWorkloadIdentityTokenExchangeAuthErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"error":"invalid identity assertion"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchModelsWithAuth(context.Background(), srv.URL, "official-openai", provider.AuthConfig{
		Type:          provider.AuthTypeWorkloadIdentity,
		IdentityToken: "expired-oidc-jwt",
		IdentityEnv:   "OPENAI_IDENTITY_TOKEN",
		Extra: map[string]string{
			"identity_provider_id": "wip_openai",
			"service_account_id":   "svc_openai",
			"token_url":            srv.URL + "/oauth/token",
		},
	})
	if err == nil {
		t.Fatal("expected token exchange auth error")
	}
	var authErr *provider.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %[1]v, want provider.AuthError", err)
	}
	if authErr.Provider != "official-openai" || authErr.KeyEnv != "OPENAI_IDENTITY_TOKEN" || authErr.Status != http.StatusUnauthorized || !authErr.HasKey {
		t.Fatalf("auth error = %+v", authErr)
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
