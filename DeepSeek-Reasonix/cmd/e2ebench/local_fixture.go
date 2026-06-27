package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
)

type localFixtureKind string

const (
	localFixtureKindOpenAI          localFixtureKind = "openai"
	localFixtureKindAnthropic       localFixtureKind = "anthropic"
	localFixtureKindFrontierUpgrade localFixtureKind = "frontier-upgrade"
	localFixtureKindOfficialAuth    localFixtureKind = "official-auth"

	localOpenAITaskID   = "local-provider-tool-loop"
	localOpenAIProvider = "local-openai-fixture"
	localOpenAIModel    = "local-tool-model"
	localOpenAIModelRef = localOpenAIProvider + "/" + localOpenAIModel
	localOpenAIKeyEnv   = "MADDOG_LOCAL_FIXTURE_KEY"

	localAnthropicTaskID   = "local-anthropic-tool-loop"
	localAnthropicProvider = "local-anthropic-fixture"
	localAnthropicModel    = "claude-local-frontier"
	localAnthropicModelRef = localAnthropicProvider + "/" + localAnthropicModel
	localAnthropicKeyEnv   = "MADDOG_LOCAL_ANTHROPIC_FIXTURE_KEY"

	localFrontierUpgradeTaskID = "local-frontier-upgrade"
	localSmallProvider         = "local-small-fixture"
	localSmallModel            = "local-small-model"
	localSmallModelRef         = localSmallProvider + "/" + localSmallModel
	localFrontierProvider      = "local-frontier-fixture"
	localFrontierModel         = "local-frontier-model"
	localFrontierModelRef      = localFrontierProvider + "/" + localFrontierModel
	localFrontierKeyEnv        = "MADDOG_LOCAL_FRONTIER_FIXTURE_KEY"

	localOfficialAuthTaskID        = "local-official-auth"
	localOfficialOpenAIProvider    = "local-openai-official"
	localOfficialOpenAIModel       = "local-openai-official-model"
	localOfficialOpenAIModelRef    = localOfficialOpenAIProvider + "/" + localOfficialOpenAIModel
	localOfficialAnthropicProvider = "local-anthropic-official"
	localOfficialAnthropicModel    = "claude-local-official"
	localOfficialAnthropicModelRef = localOfficialAnthropicProvider + "/" + localOfficialAnthropicModel
	localOfficialOpenAITokenEnv    = "MADDOG_LOCAL_OPENAI_OFFICIAL_TOKEN"
	localOfficialIdentityEnv       = "MADDOG_LOCAL_ANTHROPIC_IDENTITY_TOKEN"
)

type localFixtureSuite struct {
	server *httptest.Server
	dir    string
	bin    string
	model  string
	task   task
	envs   []localFixtureEnv
}

type localFixtureEnv struct {
	key string
	old string
	had bool
}

func newLocalFixtureSuite(tasks []task, bin string, kinds ...localFixtureKind) (*localFixtureSuite, error) {
	if len(kinds) == 0 {
		kinds = []localFixtureKind{localFixtureKindOpenAI}
	}
	kind := kinds[0]
	spec, err := localFixtureSpecFor(kind)
	if err != nil {
		return nil, err
	}
	selected, ok := findTask(tasks, spec.taskID)
	if !ok {
		return nil, fmt.Errorf("task %q is required for local fixture mode", spec.taskID)
	}
	dir, err := os.MkdirTemp("", "maddog-local-fixture-suite-")
	if err != nil {
		return nil, err
	}
	taskDir := filepath.Join(dir, "tasks", selected.ID)
	if err := copyDir(selected.dir, taskDir); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("copy local fixture task: %w", err)
	}
	workDir := filepath.Join(taskDir, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	server := spec.newServer(workDir)
	if err := writeLocalFixtureConfig(workDir, spec, server.URL); err != nil {
		server.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	selected.dir = taskDir
	var envs []localFixtureEnv
	for _, env := range spec.envs {
		oldKey, hadKey := os.LookupEnv(env.key)
		value := env.value
		if value == "" {
			value = "local-fixture-key"
		}
		if err := os.Setenv(env.key, value); err != nil {
			server.Close()
			os.RemoveAll(dir)
			restoreLocalFixtureEnvs(envs)
			return nil, err
		}
		envs = append(envs, localFixtureEnv{key: env.key, old: oldKey, had: hadKey})
	}
	return &localFixtureSuite{
		server: server,
		dir:    dir,
		bin:    bin,
		model:  spec.modelRef,
		task:   selected,
		envs:   envs,
	}, nil
}

func newLocalFixtureSuites(tasks []task, bin string) ([]*localFixtureSuite, error) {
	var suites []*localFixtureSuite
	for _, kind := range []localFixtureKind{localFixtureKindOpenAI, localFixtureKindAnthropic, localFixtureKindFrontierUpgrade, localFixtureKindOfficialAuth} {
		suite, err := newLocalFixtureSuite(tasks, bin, kind)
		if err != nil {
			for _, existing := range suites {
				existing.Close()
			}
			return nil, err
		}
		suites = append(suites, suite)
	}
	return suites, nil
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
	restoreLocalFixtureEnvs(s.envs)
}

func restoreLocalFixtureEnvs(envs []localFixtureEnv) {
	for i := len(envs) - 1; i >= 0; i-- {
		if envs[i].had {
			_ = os.Setenv(envs[i].key, envs[i].old)
		} else {
			_ = os.Unsetenv(envs[i].key)
		}
	}
}

type localFixtureSpec struct {
	taskID      string
	provider    string
	model       string
	modelRef    string
	envs        []localFixtureEnvValue
	kind        string
	newServer   func(workDir string) *httptest.Server
	writeConfig func(workDir string, spec localFixtureSpec, baseURL string) error
}

type localFixtureEnvValue struct {
	key   string
	value string
}

func localFixtureSpecFor(kind localFixtureKind) (localFixtureSpec, error) {
	switch kind {
	case localFixtureKindOpenAI:
		return localFixtureSpec{
			taskID:    localOpenAITaskID,
			provider:  localOpenAIProvider,
			model:     localOpenAIModel,
			modelRef:  localOpenAIModelRef,
			envs:      []localFixtureEnvValue{{key: localOpenAIKeyEnv}},
			kind:      "openai",
			newServer: newLocalOpenAIFixture,
		}, nil
	case localFixtureKindAnthropic:
		return localFixtureSpec{
			taskID:    localAnthropicTaskID,
			provider:  localAnthropicProvider,
			model:     localAnthropicModel,
			modelRef:  localAnthropicModelRef,
			envs:      []localFixtureEnvValue{{key: localAnthropicKeyEnv}},
			kind:      "anthropic",
			newServer: newLocalAnthropicFixture,
		}, nil
	case localFixtureKindFrontierUpgrade:
		return localFixtureSpec{
			taskID:      localFrontierUpgradeTaskID,
			provider:    localSmallProvider,
			model:       localSmallModel,
			modelRef:    localSmallModelRef,
			envs:        []localFixtureEnvValue{{key: localFrontierKeyEnv}},
			kind:        "openai",
			newServer:   newLocalFrontierUpgradeFixture,
			writeConfig: writeLocalFrontierUpgradeConfig,
		}, nil
	case localFixtureKindOfficialAuth:
		return localFixtureSpec{
			taskID:   localOfficialAuthTaskID,
			provider: localOfficialOpenAIProvider,
			model:    localOfficialOpenAIModel,
			modelRef: localOfficialOpenAIModelRef,
			envs: []localFixtureEnvValue{
				{key: localOfficialOpenAITokenEnv, value: "openai-official-access-token"},
				{key: localOfficialIdentityEnv, value: "anthropic-local-identity-jwt"},
			},
			kind:        "openai",
			newServer:   newLocalOfficialAuthFixture,
			writeConfig: writeLocalOfficialAuthConfig,
		}, nil
	default:
		return localFixtureSpec{}, fmt.Errorf("unknown local fixture kind %q", kind)
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

func writeLocalFixtureConfig(workDir string, spec localFixtureSpec, baseURL string) error {
	if spec.writeConfig != nil {
		return spec.writeConfig(workDir, spec, baseURL)
	}
	body := fmt.Sprintf(`default_model = %q
language = "en"

[[providers]]
name = %q
kind = %q
base_url = %q
model = %q
api_key_env = %q
reasoning_protocol = "none"
no_proxy = true
`, spec.modelRef, spec.provider, spec.kind, baseURL, spec.model, spec.envs[0].key)
	return os.WriteFile(filepath.Join(workDir, "maddog.toml"), []byte(body), 0o644)
}

func writeLocalFrontierUpgradeConfig(workDir string, _ localFixtureSpec, baseURL string) error {
	body := fmt.Sprintf(`default_model = %q
language = "en"

[agent]
frontier_model = %q
upgrade_enabled = true
upgrade_threshold = 3
frontier_budget = 100000

[[providers]]
name = %q
kind = "openai"
base_url = %q
model = %q
api_key_env = %q
reasoning_protocol = "none"
no_proxy = true

[[providers]]
name = %q
kind = "openai"
base_url = %q
model = %q
api_key_env = %q
reasoning_protocol = "none"
no_proxy = true
`, localSmallModelRef, localFrontierModelRef,
		localSmallProvider, baseURL, localSmallModel, localFrontierKeyEnv,
		localFrontierProvider, baseURL, localFrontierModel, localFrontierKeyEnv)
	return os.WriteFile(filepath.Join(workDir, "maddog.toml"), []byte(body), 0o644)
}

func writeLocalOfficialAuthConfig(workDir string, _ localFixtureSpec, baseURL string) error {
	body := fmt.Sprintf(`default_model = %q
language = "en"

[agent]
frontier_model = %q
upgrade_enabled = true
upgrade_threshold = 1
frontier_budget = 100000

[[providers]]
name = %q
kind = "openai"
base_url = %q
model = %q
auth_type = "bearer"
auth_token_env = %q
no_proxy = true

[[providers]]
name = %q
kind = "anthropic"
base_url = %q
model = %q
auth_type = "workload_identity"
identity_env = %q
federation_rule_id = "fdrl_local"
organization_id = "org_local"
service_account_id = "svac_local"
no_proxy = true
`, localOfficialOpenAIModelRef, localOfficialAnthropicModelRef,
		localOfficialOpenAIProvider, baseURL, localOfficialOpenAIModel, localOfficialOpenAITokenEnv,
		localOfficialAnthropicProvider, baseURL, localOfficialAnthropicModel, localOfficialIdentityEnv)
	return os.WriteFile(filepath.Join(workDir, "maddog.toml"), []byte(body), 0o644)
}

type localProviderFixture struct {
	mu       sync.Mutex
	requests int
}

type localOfficialAuthFixture struct {
	mu                sync.Mutex
	openAIAuth        string
	exchangeSeen      bool
	exchangeBody      map[string]string
	anthropicAuth     string
	anthropicRequests int
}

type localOpenAIRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role       string `json:"role"`
		Content    any    `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		Name       string `json:"name"`
	} `json:"messages"`
}

func newLocalOpenAIFixture(_ string) *httptest.Server {
	fixture := &localProviderFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", fixture.openAIChatCompletions)
	mux.HandleFunc("/models", localOpenAIModels)
	mux.HandleFunc("/v1/models", localOpenAIModels)
	return httptest.NewServer(mux)
}

func newLocalFrontierUpgradeFixture(_ string) *httptest.Server {
	fixture := &localProviderFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", fixture.frontierUpgradeChatCompletions)
	mux.HandleFunc("/models", localFrontierUpgradeModels)
	mux.HandleFunc("/v1/models", localFrontierUpgradeModels)
	return httptest.NewServer(mux)
}

func localOpenAIModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"local-tool-model","object":"model"}]}`))
}

func localFrontierUpgradeModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"local-small-model","object":"model"},{"id":"local-frontier-model","object":"model"}]}`))
}

func newLocalOfficialAuthFixture(_ string) *httptest.Server {
	fixture := &localOfficialAuthFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", fixture.openAIChatCompletions)
	mux.HandleFunc("/v1/messages", fixture.anthropicMessages)
	mux.HandleFunc("/v1/oauth/token", fixture.anthropicTokenExchange)
	mux.HandleFunc("/models", localOfficialAuthModels)
	mux.HandleFunc("/v1/models", localOfficialAuthModels)
	return httptest.NewServer(mux)
}

func localOfficialAuthModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"local-openai-official-model","object":"model"},{"id":"claude-local-official","object":"model"}]}`))
}

func (f *localProviderFixture) openAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req localOpenAIRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.requests++
	n := f.requests
	f.mu.Unlock()

	if n > 1 && !hasOpenAIToolResult(req) {
		http.Error(w, "second fixture request did not include a tool result", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if hasOpenAIToolResult(req) {
		writeSSEData(w, map[string]any{
			"choices": []map[string]any{{
				"delta":         map[string]any{"content": "Local fixture completed."},
				"finish_reason": "stop",
			}},
		})
		writeSSEData(w, map[string]any{
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
	writeSSEData(w, map[string]any{
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
	writeSSEData(w, map[string]any{
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

func (f *localProviderFixture) frontierUpgradeChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req localOpenAIRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.requests++
	n := f.requests
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if req.Model == localFrontierModel {
		writeSSEData(w, map[string]any{
			"choices": []map[string]any{{
				"delta": map[string]any{
					"content": "Local frontier fixture recovered after small-model tool failures.",
				},
				"finish_reason": "stop",
			}},
		})
		writeSSEData(w, map[string]any{
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":            53,
				"completion_tokens":        11,
				"total_tokens":             64,
				"prompt_cache_hit_tokens":  7,
				"prompt_cache_miss_tokens": 46,
			},
		})
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	if req.Model != localSmallModel {
		writeSSEData(w, map[string]any{
			"error": map[string]any{"message": "unexpected model for local frontier fixture: " + req.Model},
		})
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	args, _ := json.Marshal(map[string]string{})
	writeSSEData(w, map[string]any{
		"choices": []map[string]any{{
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    fmt.Sprintf("call_local_small_failure_%d", n),
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
	writeSSEData(w, map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":            21 + n,
			"completion_tokens":        5,
			"total_tokens":             26 + n,
			"prompt_cache_hit_tokens":  2,
			"prompt_cache_miss_tokens": 19 + n,
		},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func (f *localOfficialAuthFixture) openAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	f.openAIAuth = r.Header.Get("Authorization")
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	args, _ := json.Marshal(map[string]string{})
	writeSSEData(w, map[string]any{
		"choices": []map[string]any{{
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    "call_official_auth_failure",
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
	writeSSEData(w, map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":     23,
			"completion_tokens": 5,
			"total_tokens":      28,
		},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func (f *localOfficialAuthFixture) anthropicTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body := map[string]string{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.exchangeSeen = true
	f.exchangeBody = body
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"access_token":"anthropic-minted-local-token","token_type":"bearer","expires_in":3600}`)
}

func (f *localOfficialAuthFixture) anthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req localAnthropicRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.anthropicAuth = r.Header.Get("Authorization")
	f.anthropicRequests++
	n := f.anthropicRequests
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if n > 1 {
		if !hasAnthropicToolResult(req) {
			http.Error(w, "second official auth request did not include an Anthropic tool_result block", http.StatusBadRequest)
			return
		}
		writeAnthropicTextFinal(w, 47, 6, 9, "Official auth fixture completed.")
		return
	}
	observations := f.observationsJSON()
	writeAnthropicSSE(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"usage": map[string]any{
				"input_tokens":                47,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     6,
				"output_tokens":               1,
			},
		},
	})
	writeAnthropicSSE(w, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "tool_use",
			"id":   "toolu_official_auth_observations",
			"name": "write_file",
		},
	})
	writeAnthropicSSE(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": string(mustJSON(map[string]string{"path": "auth-fixture-observations.json", "content": observations + "\n"})),
		},
	})
	writeAnthropicSSE(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	writeAnthropicSSE(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": "tool_use",
		},
		"usage": map[string]any{
			"output_tokens": 16,
		},
	})
	writeAnthropicSSE(w, "message_stop", map[string]any{
		"type": "message_stop",
	})
}

func (f *localOfficialAuthFixture) observationsJSON() string {
	f.mu.Lock()
	data := map[string]any{
		"openai_authorization":    f.openAIAuth,
		"anthropic_authorization": f.anthropicAuth,
		"exchange_seen":           f.exchangeSeen,
		"exchange_body":           f.exchangeBody,
	}
	f.mu.Unlock()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func hasOpenAIToolResult(req localOpenAIRequest) bool {
	for _, msg := range req.Messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			return true
		}
	}
	return false
}

type localAnthropicRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
		} `json:"content"`
	} `json:"messages"`
}

func newLocalAnthropicFixture(_ string) *httptest.Server {
	fixture := &localProviderFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", fixture.anthropicMessages)
	mux.HandleFunc("/models", localAnthropicModels)
	mux.HandleFunc("/v1/models", localAnthropicModels)
	return httptest.NewServer(mux)
}

func localAnthropicModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":[{"id":"claude-local-frontier","type":"model"}]}`))
}

func (f *localProviderFixture) anthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req localAnthropicRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.requests++
	n := f.requests
	f.mu.Unlock()

	if n > 1 && !hasAnthropicToolResult(req) {
		http.Error(w, "second fixture request did not include an Anthropic tool_result block", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if hasAnthropicToolResult(req) {
		writeAnthropicSSE(w, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"usage": map[string]any{
					"input_tokens":                41,
					"cache_creation_input_tokens": 0,
					"cache_read_input_tokens":     9,
					"output_tokens":               1,
				},
			},
		})
		writeAnthropicSSE(w, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
			},
		})
		writeAnthropicSSE(w, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Local Anthropic fixture completed.",
			},
		})
		writeAnthropicSSE(w, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
		writeAnthropicSSE(w, "message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 8,
			},
		})
		writeAnthropicSSE(w, "message_stop", map[string]any{
			"type": "message_stop",
		})
		return
	}

	writeAnthropicSSE(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"usage": map[string]any{
				"input_tokens":                37,
				"cache_creation_input_tokens": 3,
				"cache_read_input_tokens":     5,
				"output_tokens":               1,
			},
		},
	})
	writeAnthropicSSE(w, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "tool_use",
			"id":   "toolu_local_fixture_write",
			"name": "write_file",
		},
	})
	writeAnthropicSSE(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": `{"path":"anthropic-fixture-output.txt",`,
		},
	})
	writeAnthropicSSE(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": `"content":"MADDOG_LOCAL_ANTHROPIC_TOOL_LOOP_OK\n"}`,
		},
	})
	writeAnthropicSSE(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	writeAnthropicSSE(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": "tool_use",
		},
		"usage": map[string]any{
			"output_tokens": 18,
		},
	})
	writeAnthropicSSE(w, "message_stop", map[string]any{
		"type": "message_stop",
	})
}

func hasAnthropicToolResult(req localAnthropicRequest) bool {
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				return true
			}
		}
	}
	return false
}

func writeAnthropicSSE(w http.ResponseWriter, event string, payload any) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	writeSSEData(w, payload)
}

func writeAnthropicTextFinal(w http.ResponseWriter, inputTokens, cacheReadTokens, outputTokens int, text string) {
	writeAnthropicSSE(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"usage": map[string]any{
				"input_tokens":                inputTokens,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     cacheReadTokens,
				"output_tokens":               1,
			},
		},
	})
	writeAnthropicSSE(w, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "text",
		},
	})
	writeAnthropicSSE(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	})
	writeAnthropicSSE(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	writeAnthropicSSE(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": "end_turn",
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})
	writeAnthropicSSE(w, "message_stop", map[string]any{
		"type": "message_stop",
	})
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func writeSSEData(w http.ResponseWriter, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
