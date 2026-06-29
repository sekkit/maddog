package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"maddog/internal/provider"
)

type modelFetchStatusError struct {
	status int
	body   string
}

func (e modelFetchStatusError) Error() string {
	return fmt.Sprintf("fetch models: status %d: %s", e.status, strings.TrimSpace(e.body))
}

// IsModelFetchEndpointMiss reports whether a model-list request reached a
// plausible endpoint path that the provider does not implement.
func IsModelFetchEndpointMiss(err error) bool {
	var statusErr modelFetchStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.status == http.StatusNotFound || statusErr.status == http.StatusMethodNotAllowed
}

// FetchModels calls the OpenAI-compatible GET /models endpoint and returns the
// available model IDs.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	return FetchModelsWithAuth(ctx, baseURL, "openai", provider.AuthConfig{Type: provider.AuthTypeAPIKey, Token: apiKey})
}

// FetchModelsWithAuth calls the OpenAI-compatible GET /models endpoint using
// the same auth contract as chat/completions and responses requests.
func FetchModelsWithAuth(ctx context.Context, baseURL, providerName string, auth provider.AuthConfig) ([]string, error) {
	cli := &http.Client{Timeout: 10 * time.Second}
	url := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(url, "/models") {
		url += "/models"
	}
	if strings.TrimSpace(providerName) == "" {
		providerName = "openai"
	}
	if auth.Token == "" && auth.NormalizedType() == provider.AuthTypeWorkloadIdentity {
		token, _, err := exchangeOpenAIWorkloadIdentity(ctx, cli, providerName, auth)
		if err != nil {
			return nil, err
		}
		auth.Token = token
	}
	if strings.TrimSpace(auth.Token) == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no auth token", providerName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch models: build request: %w", err)
	}
	auth.Header(req, "Authorization")
	req.Header.Set("Accept", "application/json")

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("fetch models: read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &provider.AuthError{
			Provider: providerName,
			KeyEnv:   auth.EnvName(),
			Status:   resp.StatusCode,
			HasKey:   strings.TrimSpace(auth.Token) != "",
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, modelFetchStatusError{status: resp.StatusCode, body: truncateFetchBody(string(body))}
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("fetch models: decode response: %w", err)
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func truncateFetchBody(body string) string {
	body = strings.TrimSpace(body)
	const max = 512
	if len([]rune(body)) <= max {
		return body
	}
	r := []rune(body)
	return string(r[:max]) + "..."
}
