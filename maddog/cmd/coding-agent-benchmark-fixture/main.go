package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

const benchmarkFixtureModel = "local-smoke-model"

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	ready := flag.String("ready-file", "", "write base URL to this file after listening")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	if *ready != "" {
		if err := os.WriteFile(*ready, []byte(baseURL), 0o644); err != nil {
			log.Fatalf("write ready file: %v", err)
		}
	}
	fmt.Println(baseURL)

	srv := &http.Server{Handler: newBenchmarkFixtureHandler()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

func newBenchmarkFixtureHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/models", benchmarkFixtureModels)
	mux.HandleFunc("/v1/models", benchmarkFixtureModels)
	mux.HandleFunc("/chat/completions", benchmarkFixtureChatCompletions)
	return mux
}

func benchmarkFixtureModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model"}]}`, benchmarkFixtureModel)
}

func benchmarkFixtureChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if hasBenchmarkToolResult(req.Messages) {
		writeFixtureSSEData(w, map[string]any{
			"choices": []map[string]any{{
				"delta":         map[string]any{"content": "Benchmark smoke fixture completed."},
				"finish_reason": "stop",
			}},
		})
		writeFixtureSSEData(w, benchmarkUsage(17, 6))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	args, _ := json.Marshal(map[string]string{
		"path": "math_utils.py",
		"content": strings.Join([]string{
			"def add(a, b):",
			"    \"\"\"Return the sum of a and b.\"\"\"",
			"    return a + b",
			"",
		}, "\n"),
	})
	writeFixtureSSEData(w, map[string]any{
		"choices": []map[string]any{{
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    "call_fix_add",
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
	writeFixtureSSEData(w, benchmarkUsage(23, 11))
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func hasBenchmarkToolResult(messages []struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
}) bool {
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			return true
		}
	}
	return false
}

func benchmarkUsage(prompt, completion int) map[string]any {
	return map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":            prompt,
			"completion_tokens":        completion,
			"total_tokens":             prompt + completion,
			"prompt_cache_hit_tokens":  3,
			"prompt_cache_miss_tokens": prompt - 3,
		},
	}
}

func writeFixtureSSEData(w http.ResponseWriter, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
