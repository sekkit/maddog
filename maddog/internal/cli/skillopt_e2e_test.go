package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"maddog/internal/skill"
	"maddog/internal/skillopt"
)

func TestSkillOptRealBinaryLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a real Maddog binary")
	}

	var failFirstProposal atomic.Bool
	failFirstProposal.Store(true)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		messageText := func(message json.RawMessage) string {
			var text string
			_ = json.Unmarshal(message, &text)
			return text
		}
		var system, user string
		hasToolResult := false
		for _, message := range request.Messages {
			switch message.Role {
			case "system":
				system += messageText(message.Content)
			case "user":
				user += messageText(message.Content)
			case "tool":
				hasToolResult = true
			}
		}
		if strings.Contains(system, "You optimize a reusable Maddog skill") {
			if failFirstProposal.CompareAndSwap(true, false) {
				writeSkillOptE2EText(w, "not-json")
				return
			}
			writeSkillOptE2EText(w, `{"candidate_body":"Write GOOD into answer.txt.","edits":[{"start":6,"end":9,"replacement":"GOOD"}],"rationale":"Use the verifier-compatible value."}`)
			return
		}
		if hasToolResult {
			writeSkillOptE2EText(w, "done")
			return
		}
		value := "bad"
		if strings.Contains(user, "Write GOOD into answer.txt.") {
			value = "good"
		}
		writeSkillOptE2EToolCall(w, value)
	}))
	defer server.Close()

	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binName := "maddog"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binary := filepath.Join(t.TempDir(), binName)
	build := exec.Command("go", "build", "-o", binary, "./cmd/maddog")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real Maddog binary: %v\n%s", err, output)
	}

	project := t.TempDir()
	home := t.TempDir()
	writeE2EFile(t, filepath.Join(home, ".maddog-state", ".env"), "MADDOG_SKILLOPT_E2E_KEY=fixture-key\n")
	configBody := fmt.Sprintf(`default_model = "local"

[agent]
max_steps = 4
temperature = 0

[tools]
enabled = ["read_file", "write_file"]

[skills.optimization]
enabled = true
model = "local"
proposer_model = "local"
rounds = 1
batch_size = 1
max_calls = 20
redact_artifacts = true
require_approval = true

[[providers]]
name = "local"
kind = "openai"
base_url = %q
model = "fixture-model"
api_key_env = "MADDOG_SKILLOPT_E2E_KEY"
`, server.URL)
	writeE2EFile(t, filepath.Join(project, "maddog.toml"), configBody)

	originalSkill := "---\nname: fixture-skill\ndescription: Writes the requested fixture value.\nallowed-tools: write_file\n---\n\nWrite BAD into answer.txt.\n"
	skillPath := filepath.Join(project, ".maddog", "skills", "fixture-skill", skill.SkillFile)
	writeE2EFile(t, skillPath, originalSkill)

	suite := filepath.Join(project, "bench")
	for _, id := range []string{"train-1", "validation-1", "test-1"} {
		taskDir := filepath.Join(suite, "tasks", id)
		writeE2EFile(t, filepath.Join(taskDir, "task.toml"), "prompt = \"placeholder\"\nmax_steps = 4\ntimeout_sec = 30\n")
		writeE2EFile(t, filepath.Join(taskDir, "verify.sh"), "#!/usr/bin/env bash\nset -e\ngot=$(tr -d '\\r\\n[:space:]' < answer.txt | tr '[:upper:]' '[:lower:]')\nwant=\"good\"\n[ \"$got\" = \"$want\" ]\n")
	}
	manifest := filepath.Join(suite, "dataset.toml")
	writeE2EFile(t, manifest, `id = "fixture"
[[train]]
id = "train-1"
input = "Write the fixture answer."
[[validation]]
id = "validation-1"
input = "Write the fixture answer."
[[test]]
id = "test-1"
input = "Write the fixture answer."
`)

	env := skillOptE2EEnv(home)
	runID := "real-binary-e2e"
	optimizeOutput, optimizeErr := runSkillOptE2EBinary(binary, project, env,
		"skillopt", "optimize", "--skill", "fixture-skill", "--manifest", manifest,
		"--suite", suite, "--run-id", runID, "--binary", binary, "--json")
	if optimizeErr == nil {
		t.Fatalf("first optimize unexpectedly succeeded; output:\n%s", optimizeOutput)
	}
	store := skillopt.NewJSONRunStore(filepath.Join(project, ".maddog", "skillopt", "runs"))
	paused, err := store.Load(t.Context(), runID)
	if err != nil {
		t.Fatalf("load paused checkpoint: %v\n%s", err, optimizeOutput)
	}
	if paused.Status != skillopt.StatusPaused || paused.NextRound != 1 || len(paused.Evaluations) != 1 || len(paused.Rounds) != 1 {
		t.Fatalf("paused checkpoint = status %s round %d evals %d", paused.Status, paused.NextRound, len(paused.Evaluations))
	}

	resumeOutput, err := runSkillOptE2EBinary(binary, project, env,
		"skillopt", "resume", "--run", runID, "--suite", suite, "--binary", binary, "--json")
	if err != nil {
		t.Fatalf("resume real binary run: %v\n%s", err, resumeOutput)
	}
	if !strings.Contains(resumeOutput, `"status": "completed"`) {
		t.Fatalf("resume output does not report completion:\n%s", resumeOutput)
	}
	completed, err := store.Load(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != skillopt.StatusCompleted || completed.LastError != "" || !completed.Test.Completed || completed.BestRevisionID == completed.BaselineRevisionID {
		t.Fatalf("completed checkpoint = %+v", completed)
	}
	if requests.Load() < 10 {
		t.Fatalf("local provider requests = %d, want full rollout/proposal lifecycle", requests.Load())
	}

	statusOutput, err := runSkillOptE2EBinary(binary, project, env, "skillopt", "status", "--run", runID, "--json")
	if err != nil || !strings.Contains(statusOutput, `"status": "completed"`) {
		t.Fatalf("status: %v\n%s", err, statusOutput)
	}
	promoteOutput, err := runSkillOptE2EBinary(binary, project, env, "skillopt", "promote", "--run", runID, "--yes", "--json")
	if err != nil {
		t.Fatalf("promote: %v\n%s", err, promoteOutput)
	}
	promoted, err := os.ReadFile(skillPath)
	if err != nil || !strings.Contains(string(promoted), "Write GOOD into answer.txt.") {
		t.Fatalf("promoted skill: %v\n%s", err, promoted)
	}
	rollbackOutput, err := runSkillOptE2EBinary(binary, project, env,
		"skillopt", "rollback", "--run", runID, "--yes", "--reason", "e2e verification")
	if err != nil {
		t.Fatalf("rollback: %v\n%s", err, rollbackOutput)
	}
	restored, err := os.ReadFile(skillPath)
	if err != nil || string(restored) != originalSkill {
		t.Fatalf("rollback did not restore exact bytes: %v\n%q", err, restored)
	}
}

func writeSkillOptE2EText(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	event, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": content}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", event)
}

func writeSkillOptE2EToolCall(w http.ResponseWriter, value string) {
	w.Header().Set("Content-Type", "text/event-stream")
	arguments, _ := json.Marshal(map[string]string{"path": "answer.txt", "content": value})
	event, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "call-write", "type": "function",
				"function": map[string]any{"name": "write_file", "arguments": string(arguments)},
			}}},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", event)
}

func writeE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func skillOptE2EEnv(home string) []string {
	overrides := map[string]string{
		"HOME":                     home,
		"USERPROFILE":              home,
		"APPDATA":                  filepath.Join(home, "AppData"),
		"XDG_CONFIG_HOME":          filepath.Join(home, ".config"),
		"MADDOG_HOME":              filepath.Join(home, ".maddog"),
		"MADDOG_STATE_HOME":        filepath.Join(home, ".maddog-state"),
		"MADDOG_CACHE_HOME":        filepath.Join(home, ".maddog-cache"),
		"MADDOG_CREDENTIALS_STORE": "file",
		"MADDOG_SKILLOPT_E2E_KEY":  "fixture-key",
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[strings.ToUpper(key)]; !overridden {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func runSkillOptE2EBinary(binary, dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	return string(output), err
}
