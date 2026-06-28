package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBenchmarkFixtureServesOpenAICompatibleToolLoop(t *testing.T) {
	srv := httptest.NewServer(newBenchmarkFixtureHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/models")
	if err != nil {
		t.Fatalf("GET /models: %v", err)
	}
	models, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(models), benchmarkFixtureModel) {
		t.Fatalf("/models status=%d body=%s", resp.StatusCode, string(models))
	}

	first := postChat(t, srv.URL, `{"model":"local-smoke-model","messages":[{"role":"user","content":"fix add"}]}`)
	for _, want := range []string{"write_file", "math_utils.py", "return a + b"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first response missing %q:\n%s", want, first)
		}
	}

	second := postChat(t, srv.URL, `{"model":"local-smoke-model","messages":[{"role":"tool","tool_call_id":"call_fix_add","name":"write_file","content":"ok"}]}`)
	if !strings.Contains(second, "Benchmark smoke fixture completed.") {
		t.Fatalf("second response did not finish turn:\n%s", second)
	}
}

func postChat(t *testing.T, baseURL, body string) string {
	t.Helper()
	resp, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /chat/completions: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /chat/completions status=%d body=%s", resp.StatusCode, string(data))
	}
	return string(data)
}
