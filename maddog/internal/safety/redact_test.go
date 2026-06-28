package safety

import (
	"strings"
	"testing"
)

func TestRedactStringRemovesCredentialValues(t *testing.T) {
	input := strings.Join([]string{
		`Authorization: Bearer sk-live-secret`,
		`api_key = "sk-config-secret"`,
		`OPENAI_API_KEY=sk-env-secret`,
		`ICODEEASY_TOKEN=icodeeasy-secret`,
		`oauth_token: ya29.oauth-secret`,
		`tool output echoed password=hunter2`,
	}, "\n")

	got := RedactString(input)
	for _, secret := range []string{"sk-live-secret", "sk-config-secret", "sk-env-secret", "icodeeasy-secret", "ya29.oauth-secret", "hunter2"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted output leaked %q:\n%s", secret, got)
		}
	}
	for _, label := range []string{"Authorization", "OPENAI_API_KEY", "ICODEEASY_TOKEN", "oauth_token"} {
		if !strings.Contains(got, label) {
			t.Fatalf("redacted output removed useful label %q:\n%s", label, got)
		}
	}
	if strings.Count(got, "[redacted-secret]") < 5 {
		t.Fatalf("redacted output = %q, want redaction markers", got)
	}
}

func TestRedactMapSanitizesHeadersAndNestedPayload(t *testing.T) {
	got := RedactMap(map[string]any{
		"headers": map[string]any{
			"Authorization": "Bearer secret-token",
			"X-Trace":       "keep-me",
		},
		"payload": map[string]any{
			"api_key": "sk-json-secret",
			"model":   "gpt-5",
		},
	})
	headers := got["headers"].(map[string]any)
	payload := got["payload"].(map[string]any)
	if headers["Authorization"] != "[redacted-secret]" {
		t.Fatalf("Authorization = %v, want redacted", headers["Authorization"])
	}
	if headers["X-Trace"] != "keep-me" {
		t.Fatalf("non-secret header = %v", headers["X-Trace"])
	}
	if payload["api_key"] != "[redacted-secret]" || payload["model"] != "gpt-5" {
		t.Fatalf("payload = %+v", payload)
	}
}
