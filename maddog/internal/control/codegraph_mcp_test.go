package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"maddog/internal/config"
	"maddog/internal/plugin"
	"maddog/internal/tool"
)

func TestConnectCodegraphMCPServerForRootPinsRootAndStripsPrefix(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	cwdLog := filepath.Join(t.TempDir(), "cwd")
	t.Setenv("MADDOG_TEST_CODEGRAPH_MCP", "1")
	t.Setenv("MADDOG_TEST_CODEGRAPH_CWD_FILE", cwdLog)

	cfg := config.Default()
	cfg.Codegraph.Enabled = true
	cfg.Codegraph.Path = os.Args[0]
	cfg.CodeIntelligence.Backends = []config.CodeIntelligenceBackendConfig{{
		Name:   "serena",
		Kind:   "mcp",
		Server: "serena",
		Tools:  map[string]string{"context_pack": "mcp__serena__context"},
	}}
	reg := tool.NewRegistry()
	c := New(Options{Host: plugin.NewHost(), Registry: reg})
	defer c.Close()

	if _, err := c.ConnectCodegraphMCPServerForRoot(cfg, projectRoot); err != nil {
		t.Fatalf("ConnectCodegraphMCPServerForRoot: %v", err)
	}
	if _, ok := reg.Get("mcp__codegraph__context"); !ok {
		t.Fatalf("stripped codegraph tool missing; names=%v", reg.Names())
	}
	if _, ok := reg.Get("mcp__codegraph__codegraph_context"); ok {
		t.Fatalf("raw codegraph prefix leaked into visible tool names; names=%v", reg.Names())
	}
	if _, ok := reg.Get("mcp__serena__context"); ok {
		t.Fatalf("external code intelligence backend should not replace or auto-register built-in codegraph tools; names=%v", reg.Names())
	}
	t.Cleanup(func() {
		c.DisconnectMCPServer("codegraph")
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, err := os.ReadFile(cwdLog); err == nil {
			got, err := filepath.EvalSymlinks(string(bytes.TrimSpace(data)))
			if err != nil {
				t.Fatal(err)
			}
			if got != wantRoot {
				t.Fatalf("codegraph cwd = %q, want %q", got, wantRoot)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper did not record cwd at %s", cwdLog)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runCodegraphMCPHelper() {
	if path := os.Getenv("MADDOG_TEST_CODEGRAPH_CWD_FILE"); path != "" {
		if cwd, err := os.Getwd(); err == nil {
			_ = os.WriteFile(path, []byte(cwd+"\n"), 0o644)
		}
	}
	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &req); err != nil || req.ID == nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "codegraph", "version": "test"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "codegraph_context",
				"description": "context",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		default:
			result = map[string]any{}
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}
}
