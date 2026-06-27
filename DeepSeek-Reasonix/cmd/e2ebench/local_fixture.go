package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
)

const (
	localFixtureTaskID   = "local-provider-tool-loop"
	localFixtureProvider = "local-openai-fixture"
	localFixtureModel    = "local-tool-model"
	localFixtureModelRef = localFixtureProvider + "/" + localFixtureModel
	localFixtureKeyEnv   = "MADDOG_LOCAL_FIXTURE_KEY"
)

type localFixtureSuite struct {
	server *httptest.Server
	dir    string
	bin    string
	model  string
	task   task
	envOld string
	envHad bool
}

func newLocalFixtureSuite(tasks []task, bin, _ string) (*localFixtureSuite, error) {
	selected, ok := findTask(tasks, localFixtureTaskID)
	if !ok {
		return nil, fmt.Errorf("task %q is required for local fixture mode", localFixtureTaskID)
	}
	server := newLocalProviderFixture()
	dir, err := os.MkdirTemp("", "maddog-local-fixture-suite-")
	if err != nil {
		server.Close()
		return nil, err
	}
	taskDir := filepath.Join(dir, "tasks", selected.ID)
	if err := copyDir(selected.dir, taskDir); err != nil {
		server.Close()
		os.RemoveAll(dir)
		return nil, fmt.Errorf("copy local fixture task: %w", err)
	}
	workDir := filepath.Join(taskDir, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		server.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	if err := writeLocalFixtureConfig(workDir, server.URL); err != nil {
		server.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	selected.dir = taskDir
	oldKey, hadKey := os.LookupEnv(localFixtureKeyEnv)
	if err := os.Setenv(localFixtureKeyEnv, "local-fixture-key"); err != nil {
		server.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	return &localFixtureSuite{
		server: server,
		dir:    dir,
		bin:    bin,
		model:  localFixtureModelRef,
		task:   selected,
		envOld: oldKey,
		envHad: hadKey,
	}, nil
}

func (s *localFixtureSuite) Close() {
	if s == nil {
		return
	}
	if s.server != nil {
		s.server.Close()
	}
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
	if s.envHad {
		_ = os.Setenv(localFixtureKeyEnv, s.envOld)
	} else {
		_ = os.Unsetenv(localFixtureKeyEnv)
	}
}

func findTask(tasks []task, id string) (task, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return task{}, false
}

func writeLocalFixtureConfig(workDir, baseURL string) error {
	body := fmt.Sprintf(`default_model = %q
language = "en"

[[providers]]
name = %q
kind = "openai"
base_url = %q
model = %q
api_key_env = %q
reasoning_protocol = "none"
no_proxy = true
`, localFixtureModelRef, localFixtureProvider, baseURL, localFixtureModel, localFixtureKeyEnv)
	return os.WriteFile(filepath.Join(workDir, "maddog.toml"), []byte(body), 0o644)
}

type localProviderFixture struct {
	mu       sync.Mutex
	requests int
}

type localFixtureRequest struct {
	Messages []struct {
		Role       string `json:"role"`
		Content    any    `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		Name       string `json:"name"`
	} `json:"messages"`
}

func newLocalProviderFixture() *httptest.Server {
	fixture := &localProviderFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", fixture.chatCompletions)
	mux.HandleFunc("/models", localFixtureModels)
	mux.HandleFunc("/v1/models", localFixtureModels)
	return httptest.NewServer(mux)
}

func localFixtureModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"local-tool-model","object":"model"}]}`))
}

func (f *localProviderFixture) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req localFixtureRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.requests++
	n := f.requests
	f.mu.Unlock()

	if n > 1 && !hasToolResult(req) {
		http.Error(w, "second fixture request did not include a tool result", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if hasToolResult(req) {
		writeSSE(w, map[string]any{
			"choices": []map[string]any{{
				"delta":         map[string]any{"content": "Local fixture completed."},
				"finish_reason": "stop",
			}},
		})
		writeSSE(w, map[string]any{
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":     31,
				"completion_tokens": 7,
				"total_tokens":      38,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 5,
				},
			},
		})
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	args, _ := json.Marshal(map[string]string{
		"path":    "fixture-output.txt",
		"content": "MADDOG_LOCAL_FIXTURE_TOOL_LOOP_OK\n",
	})
	writeSSE(w, map[string]any{
		"choices": []map[string]any{{
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    "call_local_fixture_write",
					"type":  "function",
					"function": map[string]any{
						"name":      "write_file",
						"arguments": string(args),
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
	})
	writeSSE(w, map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":            29,
			"completion_tokens":        13,
			"total_tokens":             42,
			"prompt_cache_hit_tokens":  3,
			"prompt_cache_miss_tokens": 26,
		},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func hasToolResult(req localFixtureRequest) bool {
	for _, msg := range req.Messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			return true
		}
	}
	return false
}

func writeSSE(w http.ResponseWriter, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
