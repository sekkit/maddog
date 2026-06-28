package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"maddog/internal/provider"
	"maddog/internal/safety"
)

type ModelFetchErrorCategory string

const (
	ModelFetchAuthFailure         ModelFetchErrorCategory = "auth_failure"
	ModelFetchProviderUnavailable ModelFetchErrorCategory = "provider_unavailable"
	ModelFetchEndpointMissing     ModelFetchErrorCategory = "endpoint_missing"
	ModelFetchInvalidResponse     ModelFetchErrorCategory = "invalid_response"
	ModelFetchRequestFailed       ModelFetchErrorCategory = "request_failed"
)

type modelFetchStatusError struct {
	status int
	body   string
}

func (e modelFetchStatusError) Error() string {
	return fmt.Sprintf("fetch models: %s: status %d: %s", modelFetchCategoryForStatus(e.status), e.status, strings.TrimSpace(e.body))
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

func ClassifyModelFetchError(err error) ModelFetchErrorCategory {
	if err == nil {
		return ""
	}
	var statusErr modelFetchStatusError
	if errors.As(err, &statusErr) {
		return modelFetchCategoryForStatus(statusErr.status)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ModelFetchProviderUnavailable
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ModelFetchProviderUnavailable
	}
	if strings.Contains(strings.ToLower(err.Error()), "decode response") {
		return ModelFetchInvalidResponse
	}
	return ModelFetchRequestFailed
}

func modelFetchCategoryForStatus(status int) ModelFetchErrorCategory {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ModelFetchAuthFailure
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		return ModelFetchEndpointMissing
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return ModelFetchProviderUnavailable
	default:
		return ModelFetchRequestFailed
	}
}

// FetchModels calls the OpenAI-compatible GET /models endpoint and returns the
// available model IDs.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	return FetchModelsWithAuth(ctx, baseURL, provider.AuthConfig{Type: provider.AuthTypeAPIKey, Token: apiKey}, "Authorization")
}

// FetchModelsWithAuth calls GET /models using the same auth contract as chat
// providers, so Settings probes match the saved provider auth mode.
func FetchModelsWithAuth(ctx context.Context, baseURL string, auth provider.AuthConfig, defaultAPIKeyHeader string) ([]string, error) {
	cli := &http.Client{Timeout: 10 * time.Second}
	url := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(url, "/models") {
		url += "/models"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch models: build request: %w", err)
	}
	auth.Header(req, defaultAPIKeyHeader)
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

	if resp.StatusCode != http.StatusOK {
		return nil, modelFetchStatusError{status: resp.StatusCode, body: safety.RedactString(truncateFetchBody(string(body)))}
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
