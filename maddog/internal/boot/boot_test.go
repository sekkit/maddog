package boot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/agent/testutil"
	"maddog/internal/codegraph"
	"maddog/internal/config"
	"maddog/internal/event"
	"maddog/internal/mcptrust"
	"maddog/internal/memory"
	"maddog/internal/netclient"
	"maddog/internal/plugin"
	"maddog/internal/provider"
	"maddog/internal/sandbox"
	"maddog/internal/skill"
	"maddog/internal/tool"
	"maddog/internal/tool/builtin"

	// Blank import registers the provider kind the same way cmd/maddog's main
	// does; importing builtin above registers the built-in tools.
	_ "maddog/internal/provider/openai"
)

type recordSink struct {
	mu  sync.Mutex
	evs []event.Event
}

func (s *recordSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, e)
}

func (s *recordSink) kinds(k event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

const bootImageFallbackTestProviderKind = "boot-image-fallback-test"
const bootClientProfileForwardTestProviderKind = "boot-client-profile-forward-test"

var (
	bootImageFallbackTestProviderOnce    sync.Once
	bootImageFallbackTestProviderCurrent map[string]*bootImageFallbackTestProvider
	bootImageFallbackTestProviderMu      sync.Mutex

	bootClientProfileForwardTestProviderOnce sync.Once
	bootClientProfileForwardTestProviderMu   sync.Mutex
	bootClientProfileForwardTestLastConfig   provider.Config
)

func registerBootImageFallbackTestProvider() {
	bootImageFallbackTestProviderOnce.Do(func() {
		provider.Register(bootImageFallbackTestProviderKind, func(cfg provider.Config) (provider.Provider, error) {
			bootImageFallbackTestProviderMu.Lock()
			defer bootImageFallbackTestProviderMu.Unlock()
			if bootImageFallbackTestProviderCurrent == nil {
				return nil, errors.New("boot image fallback test provider is not installed")
			}
			p := bootImageFallbackTestProviderCurrent[cfg.Name]
			if p == nil {
				return nil, fmt.Errorf("boot image fallback test provider %q is not installed", cfg.Name)
			}
			return p, nil
		})
	})
}

func registerBootClientProfileForwardTestProvider() {
	bootClientProfileForwardTestProviderOnce.Do(func() {
		provider.Register(bootClientProfileForwardTestProviderKind, func(cfg provider.Config) (provider.Provider, error) {
			bootClientProfileForwardTestProviderMu.Lock()
			bootClientProfileForwardTestLastConfig = cfg
			bootClientProfileForwardTestProviderMu.Unlock()
			return bootClientProfileForwardTestProvider{name: cfg.Name}, nil
		})
	})
}

type bootClientProfileForwardTestProvider struct {
	name string
}

func (p bootClientProfileForwardTestProvider) Name() string { return p.name }

func (p bootClientProfileForwardTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func setBootImageFallbackTestProviders(t *testing.T, providers map[string]*bootImageFallbackTestProvider) {
	t.Helper()
	bootImageFallbackTestProviderMu.Lock()
	bootImageFallbackTestProviderCurrent = providers
	bootImageFallbackTestProviderMu.Unlock()
	t.Cleanup(func() {
		bootImageFallbackTestProviderMu.Lock()
		if reflect.DeepEqual(bootImageFallbackTestProviderCurrent, providers) {
			bootImageFallbackTestProviderCurrent = nil
		}
		bootImageFallbackTestProviderMu.Unlock()
	})
}

type bootImageFallbackTestProvider struct {
	name string
	text string

	mu       sync.Mutex
	requests []provider.Request
}

func (p *bootImageFallbackTestProvider) Name() string { return p.name }

func (p *bootImageFallbackTestProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	text := p.text
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 2)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
		case ch <- provider.Chunk{Type: provider.ChunkText, Text: text}:
			ch <- provider.Chunk{Type: provider.ChunkDone}
		}
	}()
	return ch, nil
}

func (p *bootImageFallbackTestProvider) requestsSnapshot() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.Request, len(p.requests))
	copy(out, p.requests)
	return out
}

func lastUserMessage(t *testing.T, req provider.Request) provider.Message {
	t.Helper()
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == provider.RoleUser {
			return req.Messages[i]
		}
	}
	t.Fatal("request has no user message")
	return provider.Message{}
}

func bootTestBase64(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestBuildFoldsProjectMemoryIntoSystemPrompt is the end-to-end proof of the
// cache-first wiring: a project MADDOG.md is discovered at boot and folded
// into the session's system message (the cached prefix), and the `remember`
// tool is registered. It builds a real Controller from a throwaway project dir.
func TestBuildFoldsProjectMemoryIntoSystemPrompt(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)
	writeFile(t, dir, "MADDOG.md", "Project rule: always run go vet before committing.")

	ctrl, err := Build(context.Background(), Options{}) // RequireKey false: no network/key needed
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// The system message is the cached prefix; it must contain both the base
	// prompt and the discovered memory.
	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "BASE SYSTEM PROMPT") {
		t.Fatalf("base prompt missing from system message:\n%s", sys)
	}
	if !strings.Contains(sys, "always run go vet before committing") {
		t.Fatalf("project MADDOG.md not folded into system message:\n%s", sys)
	}
	// Base must come first so it stays a valid cache prefix when memory changes.
	if strings.Index(sys, "BASE SYSTEM PROMPT") > strings.Index(sys, "always run go vet") {
		t.Fatalf("memory should follow the base prompt, not precede it:\n%s", sys)
	}

	if mem := ctrl.Memory(); mem == nil || len(mem.Docs) == 0 {
		t.Fatal("controller memory set is empty after discovering MADDOG.md")
	}
}

func TestBuildWiresImageFallbackProvider(t *testing.T) {
	isolateConfigHome(t)
	registerBootImageFallbackTestProvider()
	dir := robustTempDir(t)
	t.Chdir(dir)

	mainProvider := &bootImageFallbackTestProvider{name: "main", text: "main answer"}
	visionProvider := &bootImageFallbackTestProvider{name: "vision", text: "vision summary: blue toolbar"}
	setBootImageFallbackTestProviders(t, map[string]*bootImageFallbackTestProvider{
		"main":   mainProvider,
		"vision": visionProvider,
	})

	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "main"

[agent]
system_prompt = "BASE"
image_fallback_model = "vision"

[[providers]]
name = "main"
kind = %q
base_url = "https://main.example.invalid"
model = "text-only"

[[providers]]
name = "vision"
kind = %q
base_url = "https://vision.example.invalid"
model = "vision-small"
vision = true
`, bootImageFallbackTestProviderKind, bootImageFallbackTestProviderKind))
	if err := os.WriteFile(filepath.Join(dir, "diagram.png"), bootTestBase64(t, "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="), 0o644); err != nil {
		t.Fatal(err)
	}

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "align @diagram.png"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	visionReqs := visionProvider.requestsSnapshot()
	if len(visionReqs) != 1 {
		t.Fatalf("vision provider requests = %d, want 1", len(visionReqs))
	}
	visionUser := lastUserMessage(t, visionReqs[0])
	if len(visionUser.Images) != 1 || !strings.HasPrefix(visionUser.Images[0], "data:image/png;base64,") {
		t.Fatalf("vision fallback images = %#v, want one data URL", visionUser.Images)
	}
	if !strings.Contains(visionUser.Content, "diagram.png") {
		t.Fatalf("vision fallback prompt missing image ref:\n%s", visionUser.Content)
	}

	mainReqs := mainProvider.requestsSnapshot()
	if len(mainReqs) != 1 {
		t.Fatalf("main provider requests = %d, want 1", len(mainReqs))
	}
	mainUser := lastUserMessage(t, mainReqs[0])
	if len(mainUser.Images) != 0 {
		t.Fatalf("main provider images = %#v, want none for text-only model", mainUser.Images)
	}
	if !strings.Contains(mainUser.Content, "<image_fallback") || !strings.Contains(mainUser.Content, "blue toolbar") {
		t.Fatalf("main provider prompt missing fallback summary:\n%s", mainUser.Content)
	}
	if strings.Contains(mainUser.Content, "data:image/png;base64,") {
		t.Fatalf("main provider prompt should not contain image bytes:\n%s", mainUser.Content)
	}
}

func TestBuildRunsCleanupPendingReconciler(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	called := false
	ctrl, err := Build(context.Background(), Options{
		SessionDir: sessionDir,
		CleanupPendingReconciler: func(got string) error {
			called = true
			if filepath.Clean(got) != filepath.Clean(sessionDir) {
				t.Fatalf("reconciler dir = %q, want %q", got, sessionDir)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if !called {
		t.Fatal("cleanup-pending reconciler was not called")
	}
}

func TestBuildRegistersUsableHistoryAndMemoryRetrievalTools(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-retrieval-tool-test"
model = "x"
`)

	sessionDir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	past := agent.NewSession("")
	past.Add(provider.Message{Role: provider.RoleUser, Content: "Should the history layer use vector embeddings?"})
	past.Add(provider.Message{Role: provider.RoleAssistant, Content: "Decision: port lightweight BM25 history retrieval without a vector database."})
	if err := past.Save(filepath.Join(sessionDir, "past.jsonl")); err != nil {
		t.Fatalf("save past session: %v", err)
	}

	store := memory.StoreFor(config.MemoryUserDir(), dir)
	if _, err := store.Save(memory.Memory{
		Name:        "synthesis-cache-policy",
		Description: "Stable conclusions should be reused from memory",
		Type:        memory.TypeFeedback,
		Body:        "Use a synthesis cache document when expensive retrieval produced a stable conclusion.",
	}); err != nil {
		t.Fatalf("save memory: %v", err)
	}

	registerBootRetrievalToolTestProvider()
	prov := testutil.NewMock("boot-retrieval-tool-test",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "history-1", Name: "history", Arguments: `{"operation":"search","query":"BM25 vector database","scope":"project","limit":5}`},
			{ID: "memory-1", Name: "memory", Arguments: `{"operation":"search","query":"synthesis cache stable conclusion","limit":5}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootRetrievalToolTestProvider(t, prov)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: sessionDir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	for _, forbidden := range []string{
		"Decision: port lightweight BM25 history retrieval without a vector database.",
		"Use a synthesis cache document when expensive retrieval produced a stable conclusion.",
	} {
		if strings.Contains(sys, forbidden) {
			t.Fatalf("retrieval content should stay behind on-demand tools, not enter the cache-stable system prompt:\n%s", sys)
		}
	}

	if err := ctrl.Run(context.Background(), "recover past context"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) == 0 {
		t.Fatal("provider received no requests")
	}
	for _, want := range []string{"history", "memory", "remember", "forget"} {
		if !requestHasTool(reqs[0], want) {
			t.Fatalf("first request missing tool %q; tools=%v", want, toolSchemaNames(reqs[0].Tools))
		}
	}
	assertToolOrder(t, reqs[0].Tools, []string{"forget", "history", "memory", "remember"})

	toolResults := map[string]string{}
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool {
			toolResults[msg.Name] += "\n" + msg.Content
		}
	}
	if !strings.Contains(toolResults["history"], "port lightweight BM25 history retrieval") {
		t.Fatalf("history tool result did not include saved session decision:\n%s", toolResults["history"])
	}
	if !strings.Contains(toolResults["memory"], "synthesis-cache-policy") ||
		!strings.Contains(toolResults["memory"], "stable conclusion") {
		t.Fatalf("memory tool result did not include saved memory:\n%s", toolResults["memory"])
	}
}

const bootRetrievalToolTestProviderKind = "boot-retrieval-tool-test"

var (
	bootRetrievalToolTestProviderOnce    sync.Once
	bootRetrievalToolTestProviderCurrent *testutil.MockProvider
	bootRetrievalToolTestProviderMu      sync.Mutex
)

func registerBootRetrievalToolTestProvider() {
	bootRetrievalToolTestProviderOnce.Do(func() {
		provider.Register(bootRetrievalToolTestProviderKind, func(provider.Config) (provider.Provider, error) {
			bootRetrievalToolTestProviderMu.Lock()
			defer bootRetrievalToolTestProviderMu.Unlock()
			if bootRetrievalToolTestProviderCurrent == nil {
				return nil, errors.New("boot retrieval tool test provider is not installed")
			}
			return bootRetrievalToolTestProviderCurrent, nil
		})
	})
}

func setBootRetrievalToolTestProvider(t *testing.T, p *testutil.MockProvider) {
	t.Helper()
	bootRetrievalToolTestProviderMu.Lock()
	bootRetrievalToolTestProviderCurrent = p
	bootRetrievalToolTestProviderMu.Unlock()
	t.Cleanup(func() {
		bootRetrievalToolTestProviderMu.Lock()
		if bootRetrievalToolTestProviderCurrent == p {
			bootRetrievalToolTestProviderCurrent = nil
		}
		bootRetrievalToolTestProviderMu.Unlock()
	})
}

const bootTokenProfileTestProviderKind = "boot-token-profile-test"

var (
	bootTokenProfileTestProviderOnce    sync.Once
	bootTokenProfileTestProviderCurrent *testutil.MockProvider
	bootTokenProfileTestProviderMu      sync.Mutex
)

func registerBootTokenProfileTestProvider() {
	bootTokenProfileTestProviderOnce.Do(func() {
		provider.Register(bootTokenProfileTestProviderKind, func(provider.Config) (provider.Provider, error) {
			bootTokenProfileTestProviderMu.Lock()
			defer bootTokenProfileTestProviderMu.Unlock()
			if bootTokenProfileTestProviderCurrent == nil {
				return nil, errors.New("boot token profile test provider is not installed")
			}
			return bootTokenProfileTestProviderCurrent, nil
		})
	})
}

func setBootTokenProfileTestProvider(t *testing.T, p *testutil.MockProvider) {
	t.Helper()
	bootTokenProfileTestProviderMu.Lock()
	bootTokenProfileTestProviderCurrent = p
	bootTokenProfileTestProviderMu.Unlock()
	t.Cleanup(func() {
		bootTokenProfileTestProviderMu.Lock()
		if bootTokenProfileTestProviderCurrent == p {
			bootTokenProfileTestProviderCurrent = nil
		}
		bootTokenProfileTestProviderMu.Unlock()
	})
}

func requestHasTool(req provider.Request, name string) bool {
	for _, schema := range req.Tools {
		if schema.Name == name {
			return true
		}
	}
	return false
}

func requestToolSchemaContains(req provider.Request, name, want string) bool {
	for _, schema := range req.Tools {
		if schema.Name == name {
			return strings.Contains(string(schema.Parameters), want)
		}
	}
	return false
}

func requestHasToolPrefix(req provider.Request, prefix string) bool {
	for _, schema := range req.Tools {
		if strings.HasPrefix(schema.Name, prefix) {
			return true
		}
	}
	return false
}

func toolSchemaNames(tools []provider.ToolSchema) []string {
	names := make([]string, 0, len(tools))
	for _, schema := range tools {
		names = append(names, schema.Name)
	}
	return names
}

func assertToolOrder(t *testing.T, tools []provider.ToolSchema, want []string) {
	t.Helper()
	names := toolSchemaNames(tools)
	next := 0
	for _, name := range names {
		if next < len(want) && name == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("tool order changed; provider-visible tool schema order affects prompt-cache shape.\nwant subsequence: %v\n got: %v", want, names)
	}
}

func firstTokenProfileRequest(t *testing.T, tokenMode string) provider.Request {
	t.Helper()
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-profile", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)

	opts := Options{Sink: event.Discard}
	if tokenMode != "" {
		opts.TokenMode = tokenMode
	}
	ctrl, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build(%q): %v", tokenMode, err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "capture request prefix"); err != nil {
		t.Fatalf("Run(%q): %v", tokenMode, err)
	}
	reqs := prov.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests(%q) = %d, want 1", tokenMode, len(reqs))
	}
	return reqs[0]
}

func captureTokenProfileSurface(t *testing.T, tokenMode string) (provider.Request, []tool.ContractEntry) {
	t.Helper()
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-profile", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)

	opts := Options{Sink: event.Discard}
	if tokenMode != "" {
		opts.TokenMode = tokenMode
	}
	ctrl, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build(%q): %v", tokenMode, err)
	}
	defer ctrl.Close()
	entries := ctrl.ToolContractEntries()
	if err := ctrl.Run(context.Background(), "capture request prefix"); err != nil {
		t.Fatalf("Run(%q): %v", tokenMode, err)
	}
	reqs := prov.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests(%q) = %d, want 1", tokenMode, len(reqs))
	}
	return reqs[0], entries
}

func TestBuildUsageProfileUsesResolvedDefaultModelAndEffort(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("runtime-instance", testutil.Turn{
		Text:  "done",
		Usage: &provider.Usage{PromptTokens: 8, CompletionTokens: 3, TotalTokens: 11},
	})
	setBootTokenProfileTestProvider(t, prov)
	sink := &recordSink{}

	writeFile(t, dir, "maddog.toml", `
default_model = "default-provider/chosen-model"

[codegraph]
enabled = false

[[providers]]
name = "default-provider"
kind = "boot-token-profile-test"
models = ["chosen-model", "other-model"]
default = "chosen-model"
supported_efforts = ["low", "high"]
default_effort = "high"
`)

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "capture usage profile"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	usages := sink.kinds(event.Usage)
	if len(usages) != 1 {
		t.Fatalf("usage events = %d, want 1", len(usages))
	}
	profile := usages[0].Profile
	if profile == nil {
		t.Fatal("usage profile = nil")
	}
	if profile.Role != "default" || profile.Model != "default-provider/chosen-model" || profile.Effort != "high" {
		t.Fatalf("usage profile = %+v, want default default-provider/chosen-model/high", profile)
	}
}

func TestBuildSubagentSkillFailedContinuationPersistsTranscript(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{}
	setBootSubagentTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sessionPath := agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label())
	ctrl.SetSessionPath(sessionPath)

	if err := ctrl.Run(context.Background(), "first review"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	ref := subagentRefFromHistory(t, ctrl.History())
	prov.setContinueRef(ref)

	if err := ctrl.Run(context.Background(), "continue review"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	store := agent.NewSubagentStore(filepath.Join(config.SessionDir(), "subagents"))
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != agent.SubagentFailed {
		t.Fatalf("status = %q, want failed", meta.Status)
	}
	if meta.ParentSession != agent.BranchID(sessionPath) {
		t.Fatalf("parent session = %q, want %q", meta.ParentSession, agent.BranchID(sessionPath))
	}
	sess, err := agent.LoadSession(filepath.Join(config.SessionDir(), "subagents", ref+".jsonl"))
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	msgs := sess.Snapshot()
	if len(msgs) != 4 || msgs[1].Content != "first skill task" || msgs[2].Content != "first skill answer" || msgs[3].Content != "second skill task" {
		t.Fatalf("failed skill transcript = %+v, want first task/answer plus second task", msgs)
	}
}

func TestBuildSubagentStoreHonorsSessionDirOverride(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{}
	setBootSubagentTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)

	sessionDir := filepath.Join(t.TempDir(), "desktop-workspace-sessions")
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, SessionDir: sessionDir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sessionPath := agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label())
	ctrl.SetSessionPath(sessionPath)

	if err := ctrl.Run(context.Background(), "first review"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ref := subagentRefFromHistory(t, ctrl.History())

	overrideStore := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	meta, err := overrideStore.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta from override dir: %v", err)
	}
	if meta.ParentSession != agent.BranchID(sessionPath) {
		t.Fatalf("parent session = %q, want %q", meta.ParentSession, agent.BranchID(sessionPath))
	}
	if _, err := os.Stat(filepath.Join(config.SessionDir(), "subagents", ref+".meta.json")); !os.IsNotExist(err) {
		t.Fatalf("subagent metadata should not be written to global session dir, stat err = %v", err)
	}
}

func TestBuildSubagentSkillUsesLiveReasoningLanguage(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{}
	setBootSubagentTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
reasoning_language = "zh"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetReasoningLanguage("auto")

	if err := ctrl.Run(context.Background(), "first review"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.requestsSnapshot()
	if len(reqs) < 2 {
		t.Fatalf("provider requests = %d, want parent request plus skill subagent request", len(reqs))
	}
	if got := bootLastUser(reqs[1]); strings.Contains(got, "<reasoning-language>") {
		t.Fatalf("skill subagent kept stale boot-time reasoning language after live auto update: %q", got)
	}
	if got := bootLastUser(reqs[1]); got != "first skill task" {
		t.Fatalf("skill subagent user prompt = %q, want first skill task", got)
	}
}

func TestBuildSubagentSkillForwardsUsageAsSmallProvider(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{}
	setBootSubagentTestProvider(t, prov)
	sink := &recordSink{}
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "first review"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	usages := sink.kinds(event.Usage)
	if len(usages) != 1 {
		t.Fatalf("usage events = %d, want skill subagent usage only", len(usages))
	}
	profile := usages[0].Profile
	if profile == nil || profile.Role != "small" || profile.Model != "test-model/x" {
		t.Fatalf("skill subagent usage profile = %+v, want small test-model/x", profile)
	}
	status := usages[0].ProviderStatus
	if status == nil || status.Role != "small" || status.Health != "ok" {
		t.Fatalf("skill subagent usage provider status = %+v, want small ok", status)
	}
}

func TestBuildSubagentSkillForwardsProviderStatusError(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{}
	setBootSubagentTestProvider(t, prov)
	sink := &recordSink{}
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-subagent-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	sessionPath := agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label())
	ctrl.SetSessionPath(sessionPath)

	if err := ctrl.Run(context.Background(), "first review"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	ref := subagentRefFromHistory(t, ctrl.History())
	prov.setContinueRef(ref)

	if err := ctrl.Run(context.Background(), "continue review"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	statuses := sink.kinds(event.ProviderStatusUpdate)
	if len(statuses) != 1 {
		t.Fatalf("provider status events = %d, want failed skill subagent status", len(statuses))
	}
	status := statuses[0].ProviderStatus
	if status == nil || status.Role != "small" || status.Health != "error" || status.LastError == "" {
		t.Fatalf("skill subagent provider status = %+v, want small error with last error", status)
	}
}

const bootSubagentTestProviderKind = "boot-subagent-test"

var (
	bootSubagentTestProviderOnce    sync.Once
	bootSubagentTestProviderCurrent *bootSubagentTestProvider
	bootSubagentTestProviderMu      sync.Mutex
)

func registerBootSubagentTestProvider() {
	bootSubagentTestProviderOnce.Do(func() {
		provider.Register(bootSubagentTestProviderKind, func(provider.Config) (provider.Provider, error) {
			bootSubagentTestProviderMu.Lock()
			defer bootSubagentTestProviderMu.Unlock()
			if bootSubagentTestProviderCurrent == nil {
				return nil, errors.New("boot subagent test provider is not installed")
			}
			return bootSubagentTestProviderCurrent, nil
		})
	})
}

func setBootSubagentTestProvider(t *testing.T, p *bootSubagentTestProvider) {
	t.Helper()
	bootSubagentTestProviderMu.Lock()
	bootSubagentTestProviderCurrent = p
	bootSubagentTestProviderMu.Unlock()
	t.Cleanup(func() {
		bootSubagentTestProviderMu.Lock()
		if bootSubagentTestProviderCurrent == p {
			bootSubagentTestProviderCurrent = nil
		}
		bootSubagentTestProviderMu.Unlock()
	})
}

type bootSubagentTestProvider struct {
	mu          sync.Mutex
	calls       int
	continueRef string
	requests    []provider.Request
}

func (p *bootSubagentTestProvider) Name() string { return "boot-subagent-test" }

func (p *bootSubagentTestProvider) setContinueRef(ref string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.continueRef = ref
}

func (p *bootSubagentTestProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	ref := p.continueRef
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "review-1", Name: "review", Arguments: `{"task":"first skill task"}`}}}
	case 1:
		chunks = []provider.Chunk{
			{Type: provider.ChunkText, Text: "first skill answer"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15}},
			{Type: provider.ChunkDone},
		}
	case 2:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent first done"}, {Type: provider.ChunkDone}}
	case 3:
		args, _ := json.Marshal(map[string]string{"task": "second skill task", "continue_from": ref})
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "review-2", Name: "review", Arguments: string(args)}}}
	case 4:
		chunks = []provider.Chunk{{Type: provider.ChunkError, Err: errors.New("subagent skill failed")}}
	case 5:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent second done"}, {Type: provider.ChunkDone}}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkError, Err: fmt.Errorf("unexpected provider call %d", call)}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (p *bootSubagentTestProvider) requestsSnapshot() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.Request, len(p.requests))
	copy(out, p.requests)
	return out
}

func bootLastUser(req provider.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == provider.RoleUser {
			return req.Messages[i].Content
		}
	}
	return ""
}

func subagentRefFromHistory(t *testing.T, msgs []provider.Message) string {
	t.Helper()
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool {
			continue
		}
		for _, line := range strings.Split(msg.Content, "\n") {
			if strings.HasPrefix(line, "Subagent reference: ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Subagent reference: "))
			}
		}
	}
	t.Fatalf("no subagent reference in history: %+v", msgs)
	return ""
}

// TestBuildHeadlessRunRunsTaskSubagentWithoutSessionPath reproduces headless
// `maddog run`: a controller built via Build with NO SetSessionPath (exactly
// what internal/cli.runAgent does) must still be able to run a `task` sub-agent.
// Before the ephemeral fallback this failed with "parent session is required".
func TestBuildHeadlessRunRunsTaskSubagentWithoutSessionPath(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerHeadlessTaskTestProvider()
	prov := &headlessTaskTestProvider{}
	setHeadlessTaskTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-headless-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// Deliberately NOT calling SetSessionPath — this is the headless run path.
	if err := ctrl.Run(context.Background(), "use a task subagent"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := ctrl.SessionPath(); got != "" {
		t.Fatalf("headless run should keep an empty session path, got %q", got)
	}

	var toolContent string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool {
			toolContent += "\n" + msg.Content
		}
	}
	if strings.Contains(toolContent, "parent session is required") {
		t.Fatalf("task subagent failed in headless run mode: %s", toolContent)
	}
	if !strings.Contains(toolContent, "subagent answer") {
		t.Fatalf("task tool result = %q, want sub-agent answer", toolContent)
	}
	if strings.Contains(toolContent, "Subagent reference") {
		t.Fatalf("ephemeral headless run should not persist a transcript reference: %s", toolContent)
	}
}

const headlessTaskTestProviderKind = "boot-headless-test"

var (
	headlessTaskTestProviderOnce    sync.Once
	headlessTaskTestProviderCurrent *headlessTaskTestProvider
	headlessTaskTestProviderMu      sync.Mutex
)

func registerHeadlessTaskTestProvider() {
	headlessTaskTestProviderOnce.Do(func() {
		provider.Register(headlessTaskTestProviderKind, func(provider.Config) (provider.Provider, error) {
			headlessTaskTestProviderMu.Lock()
			defer headlessTaskTestProviderMu.Unlock()
			if headlessTaskTestProviderCurrent == nil {
				return nil, errors.New("headless task test provider is not installed")
			}
			return headlessTaskTestProviderCurrent, nil
		})
	})
}

func setHeadlessTaskTestProvider(t *testing.T, p *headlessTaskTestProvider) {
	t.Helper()
	headlessTaskTestProviderMu.Lock()
	headlessTaskTestProviderCurrent = p
	headlessTaskTestProviderMu.Unlock()
	t.Cleanup(func() {
		headlessTaskTestProviderMu.Lock()
		if headlessTaskTestProviderCurrent == p {
			headlessTaskTestProviderCurrent = nil
		}
		headlessTaskTestProviderMu.Unlock()
	})
}

type headlessTaskTestProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *headlessTaskTestProvider) Name() string { return "boot-headless-test" }

func (p *headlessTaskTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()

	var chunks []provider.Chunk
	switch call {
	case 0:
		chunks = []provider.Chunk{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "task-1", Name: "task", Arguments: `{"prompt":"find callers"}`}}}
	case 1:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "subagent answer"}, {Type: provider.ChunkDone}}
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "parent done"}, {Type: provider.ChunkDone}}
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestNewProviderAppliesConfiguredDefaultEffort(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewProvider(&config.ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          srv.URL,
		Model:            "m",
		SupportedEfforts: []string{"low", "medium", "high"},
		DefaultEffort:    "MEDIUM",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got := gotReq["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium from default_effort", got)
	}
}

func TestNewProviderForwardsClientProfile(t *testing.T) {
	registerBootClientProfileForwardTestProvider()
	bootClientProfileForwardTestProviderMu.Lock()
	bootClientProfileForwardTestLastConfig = provider.Config{}
	bootClientProfileForwardTestProviderMu.Unlock()

	_, err := NewProvider(&config.ProviderEntry{
		Name:          "pxpipe-claude",
		Kind:          bootClientProfileForwardTestProviderKind,
		Model:         "claude-fable-5",
		ClientProfile: "claude_code",
		ClientVersion: "2.1.202",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	bootClientProfileForwardTestProviderMu.Lock()
	got := bootClientProfileForwardTestLastConfig
	bootClientProfileForwardTestProviderMu.Unlock()
	if got.Extra["client_profile"] != "claude_code" || got.Extra["client_version"] != "2.1.202" {
		t.Fatalf("client profile/version = %#v/%#v, want claude_code/2.1.202", got.Extra["client_profile"], got.Extra["client_version"])
	}
}

func TestNewProviderAppliesModelReasoningProtocol(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewProvider(&config.ProviderEntry{
		Name:    "deepseek-proxy",
		Kind:    "openai",
		BaseURL: srv.URL,
		Model:   "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got := gotReq["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high from DeepSeek model capability", got)
	}
	thinking, ok := gotReq["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v, want enabled", gotReq["thinking"])
	}
}

func TestBuildHonorsSessionDirOverride(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)

	sessionDir := filepath.Join(t.TempDir(), "desktop-workspace-sessions")
	ctrl, err := Build(context.Background(), Options{SessionDir: sessionDir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.SessionDir(); got != sessionDir {
		t.Fatalf("SessionDir() = %q, want override %q", got, sessionDir)
	}
}

// TestBuildDiscoversSkills proves the skill wiring end-to-end: a project skill
// is discovered at boot, surfaced via Controller.Skills(), and its name folds
// into the cache-stable system prompt's "# Skills" index alongside a built-in.
func TestBuildDiscoversSkills(t *testing.T) {
	dir := robustTempDir(t)
	isolateConfigHome(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)
	writeFile(t, dir, ".maddog/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	var hasProj, hasBuiltin bool
	for _, s := range ctrl.Skills() {
		switch s.Name {
		case "projskill":
			hasProj = true
		case "explore":
			hasBuiltin = true
		}
	}
	if !hasProj || !hasBuiltin {
		t.Fatalf("Skills() should include the project skill and a built-in; got %v", ctrl.Skills())
	}

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "# Skills") {
		t.Fatalf("skills index missing from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "projskill") || !strings.Contains(sys, "explore") {
		t.Fatalf("skill names missing from index:\n%s", sys)
	}
}

func TestBuildOfflineEvaluationIsIsolated(t *testing.T) {
	home := isolateConfigHome(t)
	dir := robustTempDir(t)
	outside := robustTempDir(t)
	custom := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("offline-eval"))

	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = true

[sandbox]
allow_write = [%q]
network = true
bash = "off"

[skills]
paths = [%q]
runtime_orchestration = true
dynamic_skills = true

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "must-not-start"
command = "missing-offline-eval-plugin"
tier = "eager"
`, outside, custom))
	writeFile(t, dir, ".maddog/skills/project.md", "---\ndescription: project candidate\n---\nproject body")
	writeFile(t, custom, "custom.md", "---\ndescription: custom skill\n---\ncustom body")
	writeFile(t, home, ".maddog/skills/global.md", "---\ndescription: global skill\n---\nglobal body")
	writeFile(t, dir, ".maddog/commands/host-command.md", "---\ndescription: host command\n---\nhost body")

	ctrl, err := Build(context.Background(), Options{OfflineEvaluation: true})
	if err != nil {
		t.Fatalf("Build offline: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.SessionDir(); got != "" {
		t.Fatalf("offline SessionDir() = %q, want in-memory session", got)
	}
	if ctrl.HookRunner().Enabled() {
		t.Fatal("offline evaluation enabled hooks")
	}
	if got := ctrl.Commands(); len(got) != 0 {
		t.Fatalf("offline commands = %+v, want none", got)
	}
	if sys := systemMessage(ctrl.History()); strings.Contains(sys, "## Environment") {
		t.Fatalf("offline system prompt contains environment probe output:\n%s", sys)
	}
	if got := ctrl.Skills(); len(got) != 1 || got[0].Name != "project" || got[0].Scope != skill.ScopeProject {
		t.Fatalf("offline skills = %+v, want only project skill", got)
	}

	reg := ctrl.ToolRegistry()
	for _, hidden := range []string{
		"web_fetch", "install_source", "remember", "forget", "memory", "list_sessions", "read_session",
		"task", "parallel_tasks", "read_only_task", "connect_tool_source", "ask", "install_skill",
	} {
		if _, ok := reg.Get(hidden); ok {
			t.Fatalf("offline evaluation exposed %q; tools=%v", hidden, reg.Names())
		}
	}
	for _, name := range reg.Names() {
		if strings.HasPrefix(name, "mcp__") || strings.HasPrefix(name, "codegraph_") {
			t.Fatalf("offline evaluation exposed external tool %q", name)
		}
	}
	if _, ok := reg.Get("bash"); ok {
		t.Fatal("offline evaluation exposed opt-in bash by default")
	}
	writeTool, ok := reg.Get("write_file")
	if !ok {
		t.Fatal("offline evaluation should retain confined write_file")
	}
	outsidePath := filepath.Join(outside, "escape.txt")
	args, _ := json.Marshal(map[string]string{"path": outsidePath, "content": "escape"})
	if _, err := writeTool.Execute(context.Background(), args); err == nil {
		t.Fatalf("offline write_file accepted configured allow_write outside workspace: %s", outsidePath)
	}
	if _, err := os.Stat(outsidePath); !os.IsNotExist(err) {
		t.Fatalf("outside file was created; stat err=%v", err)
	}
	secretPath := filepath.Join(outside, "host-secret.txt")
	if err := os.WriteFile(secretPath, []byte("must not be readable"), 0o600); err != nil {
		t.Fatal(err)
	}
	readTool, ok := reg.Get("read_file")
	if !ok {
		t.Fatal("offline evaluation should retain confined read_file")
	}
	readArgs, _ := json.Marshal(map[string]string{"path": secretPath})
	if _, err := readTool.Execute(context.Background(), readArgs); err == nil {
		t.Fatalf("offline read_file accepted host path outside workspace: %s", secretPath)
	}
}

func TestBuildTokenFullMatchesDefaultRequestPrefix(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	writeFile(t, dir, ".maddog/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	defaultReq := firstTokenProfileRequest(t, "")
	fullReq := firstTokenProfileRequest(t, TokenModeFull)

	if got, want := systemMessage(defaultReq.Messages), systemMessage(fullReq.Messages); got != want {
		t.Fatalf("explicit full mode changed the system prompt\n--- default ---\n%s\n--- full ---\n%s", got, want)
	}
	if strings.Contains(systemMessage(fullReq.Messages), tokenEconomyPrompt) {
		t.Fatalf("full mode system prompt should not include token economy prompt:\n%s", systemMessage(fullReq.Messages))
	}
	if !strings.Contains(systemMessage(fullReq.Messages), "# Skills") || !strings.Contains(systemMessage(fullReq.Messages), "projskill") {
		t.Fatalf("full mode should preserve the skills index in the system prompt:\n%s", systemMessage(fullReq.Messages))
	}
	if got, want := toolSchemaNames(fullReq.Tools), toolSchemaNames(defaultReq.Tools); !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit full mode changed tool schema order\nfull=%v\ndefault=%v", got, want)
	}
	if !reflect.DeepEqual(fullReq.Tools, defaultReq.Tools) {
		t.Fatalf("explicit full mode changed provider-visible tool schemas; names=%v", toolSchemaNames(fullReq.Tools))
	}
	if requestHasTool(fullReq, "connect_tool_source") {
		t.Fatalf("full mode should not expose economy connector; tools=%v", toolSchemaNames(fullReq.Tools))
	}
}

func TestBuildInjectsEnvironmentBlockByDefaultAndEconomy(t *testing.T) {
	for _, tokenMode := range []string{"", TokenModeEconomy} {
		t.Run(firstNonEmpty(tokenMode, "default"), func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

			req, _ := captureTokenProfileSurface(t, tokenMode)
			sys := systemMessage(req.Messages)
			if !strings.Contains(sys, "## Environment") {
				t.Fatalf("environment block missing in tokenMode=%q:\n%s", tokenMode, sys)
			}
			if !strings.Contains(sys, "- OS:") || !strings.Contains(sys, "Detected tools:") {
				t.Fatalf("environment block missing stable fields in tokenMode=%q:\n%s", tokenMode, sys)
			}
		})
	}
}

func TestBuildSkipsEnvironmentBlockWhenDisabled(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[environment]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	req, _ := captureTokenProfileSurface(t, "")
	if sys := systemMessage(req.Messages); strings.Contains(sys, "## Environment") {
		t.Fatalf("environment block should be disabled:\n%s", sys)
	}
}

func TestBuildDoesNotExecuteWorkspaceEnvironmentOverride(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	toolPath := filepath.Join(dir, "go")
	ranPath := filepath.Join(dir, "ran")
	body := "#!/bin/sh\ntouch " + shellQuoteForTest(ranPath) + "\nprintf 'bad\\n'\n"
	if runtime.GOOS == "windows" {
		toolPath += ".bat"
		body = "@echo bad>\"" + ranPath + "\"\r\n@echo bad\r\n"
	}
	if err := os.WriteFile(toolPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tool: %v", err)
	}
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[environment.tools]
go = "./go"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	req, _ := captureTokenProfileSurface(t, "")
	if _, err := os.Stat(ranPath); !os.IsNotExist(err) {
		t.Fatalf("workspace environment override was executed; stat err=%v", err)
	}
	if sys := systemMessage(req.Messages); !strings.Contains(sys, "- go: not trusted") {
		t.Fatalf("environment block should mark workspace override untrusted:\n%s", sys)
	}
}

func TestBootToolContractMatchesProviderVisibleSurface(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tokenMode string
	}{
		{name: "default", tokenMode: ""},
		{name: "economy", tokenMode: TokenModeEconomy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

			req, entries := captureTokenProfileSurface(t, tc.tokenMode)
			wantNames := defaultFullBootToolNames()
			if tc.tokenMode == TokenModeEconomy {
				wantNames = economyBootToolNames()
			}
			if got := toolSchemaNames(req.Tools); !reflect.DeepEqual(got, wantNames) {
				t.Fatalf("%s provider-visible tool surface changed\ngot  %v\nwant %v", tc.name, got, wantNames)
			}
			if len(entries) != len(req.Tools) {
				t.Fatalf("contract entries = %d, provider tools = %d\ncontract=%v\nprovider=%v", len(entries), len(req.Tools), contractEntryNames(entries), toolSchemaNames(req.Tools))
			}
			for i, e := range entries {
				s := req.Tools[i]
				if e.Name != s.Name {
					t.Fatalf("tool[%d] name = %q, want %q\ncontract=%v\nprovider=%v", i, e.Name, s.Name, contractEntryNames(entries), toolSchemaNames(req.Tools))
				}
				if e.Description != strings.TrimSpace(s.Description) {
					t.Fatalf("%s description drift\ncontract=%q\nprovider=%q", e.Name, e.Description, s.Description)
				}
				if !json.Valid(e.Schema) {
					t.Fatalf("%s contract schema is invalid JSON: %s", e.Name, e.Schema)
				}
				if got := string(provider.CanonicalizeSchema(e.Schema)); got != string(e.Schema) {
					t.Fatalf("%s contract schema is not canonical", e.Name)
				}
				if string(e.Schema) != string(s.Parameters) {
					t.Fatalf("%s schema drift\ncontract=%s\nprovider=%s", e.Name, e.Schema, s.Parameters)
				}
			}
			readOnly := map[string]bool{}
			for _, e := range entries {
				readOnly[e.Name] = e.ReadOnly
			}
			for name, want := range map[string]bool{
				"bash":                false,
				"read_file":           true,
				"memory":              true,
				"remember":            false,
				"connect_tool_source": tc.tokenMode == TokenModeEconomy,
			} {
				got, ok := readOnly[name]
				if !ok {
					if name == "connect_tool_source" && tc.tokenMode != TokenModeEconomy {
						continue
					}
					t.Fatalf("contract missing %s; tools=%v", name, contractEntryNames(entries))
				}
				if got != want {
					t.Fatalf("%s ReadOnly = %v, want %v", name, got, want)
				}
			}
		})
	}
}

func TestToolContractDocCoversDefaultBootSurfaces(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	fullReq, _ := captureTokenProfileSurface(t, TokenModeFull)
	economyReq, _ := captureTokenProfileSurface(t, TokenModeEconomy)
	doc, err := os.ReadFile(filepath.Join(pkgDir, "..", "..", "docs", "TOOL_CONTRACT.md"))
	if err != nil {
		t.Fatalf("read tool contract doc: %v", err)
	}
	text := string(doc)
	for _, heading := range []string{"## Default Full Boot Surface", "## Token Economy Boot Surface"} {
		if !strings.Contains(text, heading) {
			t.Fatalf("tool contract doc missing %q", heading)
		}
	}
	var missing []string
	for _, name := range append(toolSchemaNames(fullReq.Tools), toolSchemaNames(economyReq.Tools)...) {
		if !strings.Contains(text, "`"+name+"`") {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("tool contract doc missing boot-surface tools: %v", missing)
	}
}

func contractEntryNames(entries []tool.ContractEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names
}

func defaultFullBootToolNames() []string {
	return []string{
		"advisor",
		"ask",
		"bash",
		"bash_output",
		"code_index",
		"complete_step",
		"delete_range",
		"delete_symbol",
		"edit_file",
		"explore",
		"forget",
		"glob",
		"goal_control",
		"grep",
		"history",
		"install_skill",
		"install_source",
		"kill_shell",
		"list_sessions",
		"ls",
		"lsp_definition",
		"lsp_diagnostics",
		"lsp_hover",
		"lsp_references",
		"memory",
		"move_file",
		"multi_edit",
		"notebook_edit",
		"parallel_tasks",
		"read_file",
		"read_only_skill",
		"read_only_task",
		"read_session",
		"read_skill",
		"remember",
		"research",
		"review",
		"run_skill",
		"security_review",
		"slash_command",
		"task",
		"todo_write",
		"tool_result",
		"wait",
		"web_fetch",
		"write_file",
	}
}

func economyBootToolNames() []string {
	return []string{
		"advisor",
		"ask",
		"bash",
		"bash_output",
		"code_index",
		"complete_step",
		"connect_tool_source",
		"edit_file",
		"forget",
		"glob",
		"goal_control",
		"grep",
		"history",
		"kill_shell",
		"list_sessions",
		"ls",
		"memory",
		"move_file",
		"multi_edit",
		"read_file",
		"read_session",
		"remember",
		"slash_command",
		"todo_write",
		"tool_result",
		"wait",
		"write_file",
	}
}

func TestBuildTokenEconomyStartsWithLeanToolSurface(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "mockmcp"
command = "maddog-missing-mockmcp"
`)
	writeFile(t, dir, ".maddog/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "use the lean surface"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	req := reqs[0]
	wantTools := []string{
		"advisor",
		"ask",
		"bash",
		"bash_output",
		"code_index",
		"complete_step",
		"connect_tool_source",
		"edit_file",
		"forget",
		"glob",
		"goal_control",
		"grep",
		"history",
		"kill_shell",
		"list_sessions",
		"ls",
		"memory",
		"move_file",
		"multi_edit",
		"read_file",
		"read_session",
		"remember",
		"slash_command",
		"todo_write",
		"tool_result",
		"wait",
		"write_file",
	}
	if got := toolSchemaNames(req.Tools); !reflect.DeepEqual(got, wantTools) {
		t.Fatalf("economy first request tool order changed\ngot  %v\nwant %v", got, wantTools)
	}
	for _, want := range []string{"connect_tool_source", "read_file", "grep", "edit_file", "bash", "slash_command", "ask"} {
		if !requestHasTool(req, want) {
			t.Fatalf("economy first request missing tool %q; tools=%v", want, toolSchemaNames(req.Tools))
		}
	}
	for _, forbidden := range []string{
		"web_fetch", "task", "read_only_task", "read_only_skill", "run_skill", "read_skill", "install_skill", "install_source",
		"explore", "research", "review", "security_review",
		"lsp_definition", "lsp_references", "lsp_hover", "lsp_diagnostics",
	} {
		if requestHasTool(req, forbidden) {
			t.Fatalf("economy first request should hide %q; tools=%v", forbidden, toolSchemaNames(req.Tools))
		}
	}
	if requestHasToolPrefix(req, "mcp__mockmcp") {
		t.Fatalf("economy first request should not expose MCP placeholders; tools=%v", toolSchemaNames(req.Tools))
	}
	sys := systemMessage(req.Messages)
	if !strings.Contains(sys, tokenEconomyPrompt) {
		t.Fatalf("token economy prompt missing from system message:\n%s", sys)
	}
	if strings.Contains(sys, "# Skills") || strings.Contains(sys, "projskill") {
		t.Fatalf("skills index should not be in economy system prompt:\n%s", sys)
	}
}

func TestBuildTokenEconomyConnectsWebFetchOnDemand(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"web_fetch"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "fetch later"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if requestHasTool(reqs[0], "web_fetch") {
		t.Fatalf("first request should hide web_fetch; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	if !requestHasTool(reqs[1], "web_fetch") {
		t.Fatalf("second request should expose web_fetch after connect_tool_source; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
}

func TestBuildTokenEconomyCodegraphSetsShortDaemonIdleTimeout(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	launcher := writeCodegraphHelper(t, dir)
	envOut := filepath.Join(dir, "codegraph-idle-env")
	t.Setenv("MADDOG_CODEGRAPH_HELPER_ENV_OUT", envOut)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"codegraph"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`, launcher))

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "enable codegraph later"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if requestHasToolPrefix(reqs[0], "mcp__codegraph__") {
		t.Fatalf("first request should hide codegraph; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	if !requestHasToolPrefix(reqs[1], "mcp__codegraph__") {
		t.Fatalf("second request should expose codegraph after connect_tool_source; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	got, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatalf("read codegraph idle timeout env: %v", err)
	}
	if string(got) != codegraph.MaddogDaemonIdleTimeoutMS {
		t.Fatalf("%s = %q; want %q", codegraph.DaemonIdleTimeoutEnv, got, codegraph.MaddogDaemonIdleTimeoutMS)
	}
}

func TestBuildTokenEconomyPlanModeCanConnectWebFetch(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"web_fetch"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetPlanMode(true)
	if err := ctrl.Run(context.Background(), "fetch later while planning"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if !requestHasTool(reqs[1], "web_fetch") {
		t.Fatalf("second request should expose web_fetch in plan economy mode; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" && strings.Contains(msg.Content, "blocked:") {
			t.Fatalf("connect_tool_source should not be blocked in plan mode, got:\n%s", msg.Content)
		}
	}
}

func TestBuildTokenEconomyPlanModeCanConnectReadOnlyTask(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"read_only_subagent"}`},
		}},
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "readonly-1", Name: "read_only_task", Arguments: `{"prompt":"inspect safely"}`},
		}},
		testutil.Turn{Text: "read-only findings"},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetPlanMode(true)
	if err := ctrl.Run(context.Background(), "connect read-only subagent while planning"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want 4", len(reqs))
	}
	if !requestHasTool(reqs[1], "read_only_task") {
		t.Fatalf("second request should expose read_only_task in plan economy mode; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	if requestHasTool(reqs[1], "task") {
		t.Fatalf("read_only_task source should not expose writer-capable task; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	subReq := reqs[2]
	if !requestHasTool(subReq, "bash") || !requestHasTool(subReq, "read_file") {
		t.Fatalf("read_only_task child request should keep read-only research tools; tools=%v", toolSchemaNames(subReq.Tools))
	}
	if requestToolSchemaContains(subReq, "bash", "run_in_background") {
		t.Fatalf("read_only_task child bash schema should not advertise run_in_background")
	}
	for _, forbidden := range []string{
		"connect_tool_source", "task", "read_only_task", "parallel_tasks",
		"install_source", "run_skill", "read_only_skill", "read_skill", "install_skill", "remember", "forget",
		"write_file", "edit_file", "multi_edit", "move_file", "complete_step",
	} {
		if requestHasTool(subReq, forbidden) {
			t.Fatalf("read_only_task child request should hide %q; tools=%v", forbidden, toolSchemaNames(subReq.Tools))
		}
	}
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" && strings.Contains(msg.Content, "blocked:") {
			t.Fatalf("connect_tool_source should not block read_only_task in plan mode, got:\n%s", msg.Content)
		}
	}
}

func TestBuildTokenEconomyPlanModeCanConnectReadOnlySkill(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"read_only_skill"}`},
		}},
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "skill-1", Name: "read_only_skill", Arguments: `{"name":"readonlydig","arguments":"inspect safely"}`},
		}},
		testutil.Turn{Text: "skill findings"},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	writeFile(t, dir, ".maddog/skills/readonlydig/SKILL.md", `---
description: read-only dig
runAs: subagent
allowed-tools: read_file, bash, write_file, connect_tool_source, read_only_skill
---
READ ONLY SKILL BODY`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetPlanMode(true)
	if err := ctrl.Run(context.Background(), "connect read-only skill while planning"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want 4", len(reqs))
	}
	if !requestHasTool(reqs[1], "read_only_skill") {
		t.Fatalf("second request should expose read_only_skill in plan economy mode; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	for _, forbidden := range []string{"run_skill", "read_skill", "install_skill", "task", "review", "install_source"} {
		if requestHasTool(reqs[1], forbidden) {
			t.Fatalf("read_only_skill source should not expose %q; tools=%v", forbidden, toolSchemaNames(reqs[1].Tools))
		}
	}
	subReq := reqs[2]
	if !strings.Contains(systemMessage(subReq.Messages), "READ ONLY SKILL BODY") {
		t.Fatalf("read_only_skill child should use the skill body as system prompt:\n%s", systemMessage(subReq.Messages))
	}
	if !requestHasTool(subReq, "bash") || !requestHasTool(subReq, "read_file") {
		t.Fatalf("read_only_skill child request should keep read-only research tools; tools=%v", toolSchemaNames(subReq.Tools))
	}
	if requestToolSchemaContains(subReq, "bash", "run_in_background") {
		t.Fatalf("read_only_skill child bash schema should not advertise run_in_background")
	}
	for _, forbidden := range []string{
		"connect_tool_source", "task", "read_only_task", "read_only_skill", "parallel_tasks",
		"install_source", "run_skill", "read_skill", "install_skill", "remember", "forget",
		"write_file", "edit_file", "multi_edit", "move_file", "complete_step",
	} {
		if requestHasTool(subReq, forbidden) {
			t.Fatalf("read_only_skill child request should hide %q; tools=%v", forbidden, toolSchemaNames(subReq.Tools))
		}
	}
	var toolOutput string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" {
			toolOutput += msg.Content
			if strings.Contains(msg.Content, "blocked:") {
				t.Fatalf("connect_tool_source should not block read_only_skill in plan mode, got:\n%s", msg.Content)
			}
		}
	}
	if !strings.Contains(toolOutput, "readonlydig") || !strings.Contains(toolOutput, "# Skills") {
		t.Fatalf("read_only_skill source result should include the skill index, got:\n%s", toolOutput)
	}
}

func TestBuildTokenEconomyPlanModeCanConnectAllowedMCPSource(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"mcp","name":"mockmcp"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[agent]
system_prompt = "BASE"
plan_mode_allowed_tools = ["mcp__mockmcp__echo"]

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "mockmcp"
command = %q
args = ["-test.run=TestHelperProcess", "--"]
env = { GO_WANT_HELPER_PROCESS = "1" }
`, os.Args[0]))

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetPlanMode(true)
	if err := ctrl.Run(context.Background(), "connect allowed mcp while planning"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if !requestHasTool(reqs[1], "mcp__mockmcp__echo") {
		t.Fatalf("second request should expose allowed MCP source in plan economy mode; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" {
			if strings.Contains(msg.Content, "blocked:") {
				t.Fatalf("connect_tool_source should not block allowed MCP in plan mode, got:\n%s", msg.Content)
			}
			if !strings.Contains(msg.Content, `enabled MCP server "mockmcp" tools: mcp__mockmcp__echo`) {
				t.Fatalf("connect_tool_source should report enabled MCP tools, got:\n%s", msg.Content)
			}
		}
	}
}

func TestBuildTokenEconomyPlanModeCanConnectTrustedReadOnlyMCPSource(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"mcp","name":"mockmcp"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "mockmcp"
command = %q
args = ["-test.run=TestHelperProcess", "--"]
env = { GO_WANT_HELPER_PROCESS = "1" }
trusted_read_only_tools = ["echo"]
`, os.Args[0]))

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetPlanMode(true)
	if err := ctrl.Run(context.Background(), "connect trusted mcp while planning"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if !requestHasTool(reqs[1], "mcp__mockmcp__echo") {
		t.Fatalf("second request should expose trusted MCP source in plan economy mode; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" && strings.Contains(msg.Content, "blocked:") {
			t.Fatalf("connect_tool_source should not block trusted MCP in plan mode, got:\n%s", msg.Content)
		}
	}
}

func TestPlanModeAllowsMCPServerRequiresConcreteToolName(t *testing.T) {
	if planModeAllowsMCPServer([]string{"mcp__mockmcp__"}, "mockmcp") {
		t.Fatal("bare MCP namespace prefix should not allow a server in plan mode")
	}
	if !planModeAllowsMCPServer([]string{"mcp__mockmcp__echo"}, "mockmcp") {
		t.Fatal("concrete MCP tool name should allow its server in plan mode")
	}
}

func TestBuildTokenEconomyPlanModeBlocksSourcesWithPolicy(t *testing.T) {
	tests := []struct {
		source          string
		args            string
		forbiddenTools  []string
		forbiddenPrefix string
	}{
		{
			source:         "task",
			args:           `{"source":"task"}`,
			forbiddenTools: []string{"task"},
		},
		{
			source:         "install_source",
			args:           `{"source":"install_source"}`,
			forbiddenTools: []string{"install_source"},
		},
		{
			source: "skills",
			args:   `{"source":"skills"}`,
			forbiddenTools: []string{
				"run_skill", "read_only_skill", "read_skill", "install_skill",
				"explore", "research", "review", "security_review",
			},
		},
		{
			source:          "mcp",
			args:            `{"source":"mcp","name":"mockmcp"}`,
			forbiddenPrefix: "mcp__mockmcp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)

			registerBootTokenProfileTestProvider()
			prov := testutil.NewMock("token-economy",
				testutil.Turn{ToolCalls: []provider.ToolCall{
					{ID: "source-1", Name: "connect_tool_source", Arguments: tt.args},
				}},
				testutil.Turn{Text: "done"},
			)
			setBootTokenProfileTestProvider(t, prov)
			writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "mockmcp"
command = "maddog-missing-mockmcp"
`)

			ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			ctrl.SetPlanMode(true)
			if err := ctrl.Run(context.Background(), "connect blocked source while planning"); err != nil {
				t.Fatalf("Run: %v", err)
			}

			reqs := prov.Requests()
			if len(reqs) != 2 {
				t.Fatalf("requests = %d, want 2", len(reqs))
			}
			var toolOutput string
			for _, msg := range ctrl.History() {
				if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" {
					toolOutput += msg.Content
				}
			}
			if strings.TrimSpace(toolOutput) == "" {
				t.Fatalf("connect_tool_source(%s) returned empty tool output", tt.source)
			}
			if !strings.Contains(toolOutput, "blocked:") || !strings.Contains(toolOutput, "plan mode") {
				t.Fatalf("connect_tool_source(%s) output = %q, want visible plan-mode block", tt.source, toolOutput)
			}
			for _, forbidden := range tt.forbiddenTools {
				if requestHasTool(reqs[1], forbidden) {
					t.Fatalf("blocked source %s should not expose %q; tools=%v", tt.source, forbidden, toolSchemaNames(reqs[1].Tools))
				}
			}
			if tt.forbiddenPrefix != "" && requestHasToolPrefix(reqs[1], tt.forbiddenPrefix) {
				t.Fatalf("blocked source %s should not expose tools with prefix %q; tools=%v", tt.source, tt.forbiddenPrefix, toolSchemaNames(reqs[1].Tools))
			}
		})
	}
}

func TestBuildWarnsIgnoredPlanModeAllowedTools(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("plan-mode-allowed-tools", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
plan_mode_allowed_tools = ["bash", "custom_reader"]

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, notice := range notices {
		if notice.Level == event.LevelWarn && strings.Contains(notice.Text, "plan_mode_allowed_tools") && strings.Contains(notice.Text, "bash") {
			if strings.Contains(notice.Text, "custom_reader") {
				t.Fatalf("warning should name ignored entries only, got %q", notice.Text)
			}
			return
		}
	}
	t.Fatalf("missing ignored plan_mode_allowed_tools warning; got %+v", notices)
}

func TestBuildTokenEconomyWebFetchConnectorHonorsDisabledBuiltin(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"web_fetch"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[tools]
enabled = ["read_file", "grep"]

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "fetch later"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if requestHasTool(reqs[1], "web_fetch") {
		t.Fatalf("disabled web_fetch should not be exposed after connect_tool_source; tools=%v", toolSchemaNames(reqs[1].Tools))
	}
	var toolOutput string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" {
			toolOutput += msg.Content
		}
	}
	if !strings.Contains(toolOutput, "web_fetch is disabled by [tools].enabled") {
		t.Fatalf("connector should explain disabled web_fetch, got:\n%s", toolOutput)
	}
}

func TestBuildTokenEconomyConnectsSkillsOnDemand(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("token-economy",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "source-1", Name: "connect_tool_source", Arguments: `{"source":"skills"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	writeFile(t, dir, ".maddog/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: TokenModeEconomy})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "use skills later"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	for _, name := range []string{"run_skill", "read_only_skill", "read_skill", "explore"} {
		if requestHasTool(reqs[0], name) {
			t.Fatalf("first request should hide %q; tools=%v", name, toolSchemaNames(reqs[0].Tools))
		}
		if !requestHasTool(reqs[1], name) {
			t.Fatalf("second request should expose %q after connect_tool_source; tools=%v", name, toolSchemaNames(reqs[1].Tools))
		}
	}
	var toolOutput string
	for _, msg := range ctrl.History() {
		if msg.Role == provider.RoleTool && msg.Name == "connect_tool_source" {
			toolOutput += msg.Content
		}
	}
	if !strings.Contains(toolOutput, "projskill") || !strings.Contains(toolOutput, "# Skills") {
		t.Fatalf("skills source result should include the skill index, got:\n%s", toolOutput)
	}
}

func TestAddBuiltinsWithWorkspaceRootKeepsSessionTools(t *testing.T) {
	reg := tool.NewRegistry()
	var stderr bytes.Buffer
	addBuiltins(reg, nil, []string{robustTempDir(t)}, nil, sandbox.Spec{}, 120*time.Second, builtin.SearchSpec{}, &stderr, robustTempDir(t), netclient.ProxySpec{}, nil, nil)
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
		"move_file",
		"notebook_edit",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("workspace builtins missing %q; got %v", name, reg.Names())
		}
	}
}

func TestBuildOmitsDisabledSkillsFromPromptAndRuntimeList(t *testing.T) {
	dir := robustTempDir(t)
	isolateConfigHome(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[skills]
disabled_skills = ["projskill", "review"]

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)
	writeFile(t, dir, ".maddog/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, s := range ctrl.Skills() {
		if s.Name == "projskill" || s.Name == "review" {
			t.Fatalf("disabled skill %q should not be executable: %v", s.Name, ctrl.Skills())
		}
	}
	var allHasProj bool
	for _, s := range ctrl.AllSkills() {
		if s.Name == "projskill" {
			allHasProj = true
		}
	}
	if !allHasProj {
		t.Fatalf("AllSkills should include disabled skills for management: %v", ctrl.AllSkills())
	}
	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "projskill") || strings.Contains(sys, "- review ") {
		t.Fatalf("disabled skill names should be omitted from system prompt:\n%s", sys)
	}
}

func TestBuildOmitsExcludedSkillRootsFromPromptAndRuntimeList(t *testing.T) {
	dir := robustTempDir(t)
	home := isolateConfigHome(t)
	t.Chdir(dir)
	excluded := filepath.Join(home, ".agents", "skills")
	writeFile(t, home, ".maddog/skills/keep.md", "---\ndescription: keep\n---\nplaybook")
	writeFile(t, home, ".agents/skills/noisy.md", "---\ndescription: noisy\n---\nplaybook")
	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[agent]
system_prompt = "BASE"

[skills]
excluded_paths = [%q]

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`, excluded))

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, s := range ctrl.Skills() {
		if s.Name == "noisy" {
			t.Fatalf("excluded skill should not be executable: %v", ctrl.Skills())
		}
	}
	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "noisy") {
		t.Fatalf("excluded skill name should be omitted from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "keep") {
		t.Fatalf("non-excluded skill should remain in system prompt:\n%s", sys)
	}
}

// TestBuildWithoutMemoryLeavesPromptUnchanged is the inverse invariant: with no
// memory files, the system prompt is exactly the configured base — the cache
// prefix is untouched by the memory feature.
func TestBuildWithoutMemoryLeavesPromptUnchanged(t *testing.T) {
	dir := robustTempDir(t)
	isolateConfigHome(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "JUST THE BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	// The built-in skills always append a "# Skills" index to the prefix; this
	// test is about memory, so strip that and assert the remaining base is exactly
	// the configured prompt — i.e. no *project/ancestor* memory leaked in. (A
	// user-global MADDOG.md in the real config dir could append; the test
	// environment has none, so the base stands alone.)
	base := sys
	if i := strings.Index(sys, "\n\n# Skills"); i >= 0 {
		base = sys[:i]
	}
	// The language policy is always appended at boot; strip it so this assertion
	// is purely about whether project/ancestor memory leaked into the base. The
	// user-decision policy is another fixed boot policy and is stripped for the
	// same reason.
	base = stripEnvironmentBlock(base)
	base = stripLanguagePolicy(base)
	if base != "JUST THE BASE" {
		t.Fatalf("expected untouched base prompt, got:\n%s", sys)
	}
}

func TestBuildLanguagePolicyIsAppended(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, config.LanguagePolicy) {
		t.Fatalf("language policy missing from system prompt:\n%s", sys)
	}
}

func TestBuildAppendsUserDecisionPolicyToCustomSystemPrompt(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	for _, want := range []string{
		"User-owned choices",
		"call the ask tool",
		"Do not ask in prose",
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("user decision policy missing %q from custom system prompt:\n%s", want, sys)
		}
	}
}

func systemMessage(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func stripLanguagePolicy(s string) string {
	s = strings.TrimSpace(s)
	for _, policy := range []string{
		config.LanguagePolicy,
		config.UserDecisionPolicy,
	} {
		s = strings.TrimSpace(strings.TrimSuffix(s, policy))
	}
	return s
}

func stripEnvironmentBlock(s string) string {
	if i := strings.Index(s, "\n\n## Environment"); i >= 0 {
		return s[:i]
	}
	return s
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := writeFileRaw(dir, name, body); err != nil {
		t.Fatal(err)
	}
}

func shellQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func TestRememberPermissionRuleUsesWorkspaceRoot(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := robustTempDir(t)
	workspace := robustTempDir(t)
	t.Chdir(cwd)
	writeFile(t, cwd, "maddog.toml", `
[permissions]
allow = ["Bash(cwd*)"]
`)
	writeFile(t, workspace, "maddog.toml", `
[permissions]
allow = ["Bash(workspace*)"]
`)

	const rule = "Bash(go test ./...)"
	rememberPermissionRule(workspace, rule)

	cwdCfg := config.LoadForEdit(filepath.Join(cwd, "maddog.toml"))
	if hasPermissionRule(cwdCfg.Permissions.Allow, rule) {
		t.Fatalf("remembered rule was written to cwd config: %v", cwdCfg.Permissions.Allow)
	}
	workspaceCfg := config.LoadForEdit(filepath.Join(workspace, "maddog.toml"))
	if !hasPermissionRule(workspaceCfg.Permissions.Allow, rule) {
		t.Fatalf("remembered rule missing from workspace config: %v", workspaceCfg.Permissions.Allow)
	}
}

func TestRememberPermissionRuleCreatesWorkspaceConfigOverUserConfig(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	workspace := robustTempDir(t)
	userConfig := config.UserConfigPath()
	writeFile(t, filepath.Dir(userConfig), filepath.Base(userConfig), `
[permissions]
allow = ["Bash(user)"]
`)

	const rule = "Edit(src/app.go)"
	res := rememberPermissionRule(workspace, rule)
	if !res.Saved || res.Path != filepath.Join(workspace, "maddog.toml") {
		t.Fatalf("remember result = %+v, want saved to workspace config", res)
	}

	userCfg := config.LoadForEdit(userConfig)
	if hasPermissionRule(userCfg.Permissions.Allow, rule) {
		t.Fatalf("workspace rule was written to user config: %v", userCfg.Permissions.Allow)
	}
	workspaceCfg := config.LoadForEdit(filepath.Join(workspace, "maddog.toml"))
	if !hasPermissionRule(workspaceCfg.Permissions.Allow, rule) {
		t.Fatalf("workspace rule missing from project config: %v", workspaceCfg.Permissions.Allow)
	}
}

func TestRememberPermissionRuleEmptyRootUsesSourcePath(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := robustTempDir(t)
	t.Chdir(cwd)
	userConfig := config.UserConfigPath()
	writeFile(t, filepath.Dir(userConfig), filepath.Base(userConfig), `
[permissions]
allow = ["Bash(user*)"]
`)

	const rule = "Bash(go env)"
	res := rememberPermissionRule("", rule)
	if !res.Saved || res.Path != userConfig {
		t.Fatalf("remember result = %+v, want saved to user source config", res)
	}

	userCfg := config.LoadForEdit(userConfig)
	if !hasPermissionRule(userCfg.Permissions.Allow, rule) {
		t.Fatalf("empty root should remember into SourcePath config: %v", userCfg.Permissions.Allow)
	}
	if _, err := os.Stat(filepath.Join(cwd, "maddog.toml")); !os.IsNotExist(err) {
		t.Fatalf("empty root should not create cwd config when SourcePath exists, err=%v", err)
	}
}

func TestRememberPermissionRuleSkipsRuleCoveredByExistingAllow(t *testing.T) {
	workspace := robustTempDir(t)
	writeFile(t, workspace, "maddog.toml", `
[permissions]
allow = ["Bash(go test:*)"]
`)

	res := rememberPermissionRule(workspace, "Bash(go test ./...)")
	if res.Saved || res.CoveredBy != "Bash(go test:*)" {
		t.Fatalf("remember result = %+v, want already covered", res)
	}
	cfg := config.LoadForEdit(filepath.Join(workspace, "maddog.toml"))
	if len(cfg.Permissions.Allow) != 1 || cfg.Permissions.Allow[0] != "Bash(go test:*)" {
		t.Fatalf("allow rules = %v, want only existing prefix", cfg.Permissions.Allow)
	}
}

func TestRememberPermissionRulePrunesNarrowRulesWhenSavingBroaderRule(t *testing.T) {
	workspace := robustTempDir(t)
	writeFile(t, workspace, "maddog.toml", `
[permissions]
allow = ["Bash(go test ./...)", "Bash(go build ./...)"]
`)

	res := rememberPermissionRule(workspace, "Bash(go test:*)")
	if !res.Saved || res.CoveredBy != "" {
		t.Fatalf("remember result = %+v, want saved broader rule", res)
	}
	cfg := config.LoadForEdit(filepath.Join(workspace, "maddog.toml"))
	if hasPermissionRule(cfg.Permissions.Allow, "Bash(go test ./...)") {
		t.Fatalf("narrow go test rule should be pruned: %v", cfg.Permissions.Allow)
	}
	if !hasPermissionRule(cfg.Permissions.Allow, "Bash(go build ./...)") || !hasPermissionRule(cfg.Permissions.Allow, "Bash(go test:*)") {
		t.Fatalf("allow rules = %v, want unrelated exact plus prefix", cfg.Permissions.Allow)
	}
}

func hasPermissionRule(rules []string, want string) bool {
	for _, rule := range rules {
		if rule == want {
			return true
		}
	}
	return false
}

func TestBuildDoesNotMigrateOriginalMaddogConfigEndToEnd(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                               // os.UserHomeDir on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // os.UserConfigDir on Linux
	t.Setenv("AppData", filepath.Join(home, "AppData"))         // os.UserConfigDir on Windows
	t.Setenv("DEEPSEEK_API_KEY", "")

	proj := robustTempDir(t)
	t.Chdir(proj)
	writeFile(t, proj, "maddog.toml", "[codegraph]\nenabled = false\n")
	writeFile(t, filepath.Join(home, ".maddog"), "config.json",
		`{"apiKey":"sk-e2e","lang":"zh","mcpServers":{"fs":{"command":"npx","args":["-y","server-fs"]}}}`)
	writeFile(t, filepath.Join(home, ".maddog", "sessions"), "chat-1.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from v0.x"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, n := range notices {
		if strings.Contains(n, "migrated your previous configuration") {
			t.Fatalf("Build should not migrate original Maddog config; notices=%v", notices)
		}
	}

	dest := config.UserConfigPath()
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("Build wrote Maddog user config from original Maddog install, stat err=%v", err)
	}

	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "" {
		t.Errorf("DEEPSEEK_API_KEY should not be pinned from original Maddog config: %q", got)
	}

	if _, err := os.Stat(config.UserCredentialsPath()); !os.IsNotExist(err) {
		t.Errorf("credentials store should not be written from original Maddog config, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Errorf("Build must not write the user's ~/.env, stat err=%v", err)
	}

	for _, n := range notices {
		if strings.Contains(n, "imported") && strings.Contains(n, "past session") {
			t.Fatalf("Build should not import original Maddog sessions; notices=%v", notices)
		}
	}
	migratedSession := filepath.Join(config.SessionDir(), "chat-1.jsonl")
	if _, err := os.Stat(migratedSession); !os.IsNotExist(err) {
		t.Errorf("original Maddog session should not be imported to %s, stat err=%v", migratedSession, err)
	}
}

func TestBuildMigratesLegacySessionsFromConfigSessionDir(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	proj := robustTempDir(t)
	writeFile(t, proj, "maddog.toml", "[codegraph]\nenabled = false\n")

	legacyConfig := config.LegacyUserConfigPath()
	if legacyConfig == "" {
		t.Skip("legacy OS config path matches primary path on this platform")
	}
	legacyDir := filepath.Join(filepath.Dir(legacyConfig), "sessions")
	writeFile(t, legacyDir, "custom-root.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from redirected config root"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi from redirected root","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	// Pass the project root via WorkspaceRoot instead of t.Chdir: changing the
	// process cwd into a t.TempDir makes Windows refuse to remove that dir during
	// test cleanup (the cwd counts as "in use"), which is the only thing this test
	// failed on. WorkspaceRoot loads the same config without touching the cwd.
	ctrl, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: proj})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sessionPath := filepath.Join(config.SessionDir(), "custom-root.jsonl")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("legacy config-root session not imported to %s: %v", sessionPath, err)
	}
	if !strings.Contains(string(data), "hello from redirected config root") {
		t.Fatalf("migrated session missing legacy content:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(config.SessionDir(), ".legacy-imported.v0-events-config")); err != nil {
		t.Fatalf("config-root legacy import marker missing: %v", err)
	}
	sessionImported := false
	for _, n := range notices {
		if strings.Contains(n, "imported") && strings.Contains(n, "past session") && strings.Contains(n, legacyDir) {
			sessionImported = true
		}
	}
	if !sessionImported {
		t.Errorf("no config-root session-import notice emitted; got %v", notices)
	}
}

func TestBuildMigratesLegacyXDGAndProjectSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("legacy XDG paths are Unix-only")
	}
	home := robustTempDir(t)
	xdg := filepath.Join(home, "xdg-config")
	maddogHome := filepath.Join(home, "rx-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("MADDOG_HOME", maddogHome)

	proj := robustTempDir(t)
	writeFile(t, proj, "maddog.toml", "[codegraph]\nenabled = false\n")

	legacyRoot := filepath.Join(xdg, "maddog")
	writeFile(t, filepath.Join(legacyRoot, "sessions"), "xdg-flat.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from xdg"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi from xdg","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	slug := config.WorkspaceSlug(proj)
	legacyProjectDir := filepath.Join(legacyRoot, "projects", slug, "sessions")
	session := agent.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello from old project session"})
	if err := session.Save(filepath.Join(legacyProjectDir, "project-chat.jsonl")); err != nil {
		t.Fatalf("save legacy project session: %v", err)
	}

	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: proj})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	flatData, err := os.ReadFile(filepath.Join(config.SessionDir(), "xdg-flat.jsonl"))
	if err != nil {
		t.Fatalf("legacy XDG flat session not imported: %v", err)
	}
	if !strings.Contains(string(flatData), "hello from xdg") {
		t.Fatalf("legacy XDG flat session missing content:\n%s", flatData)
	}
	projectPath := filepath.Join(config.MemoryUserDir(), "projects", slug, "sessions", "project-chat.jsonl")
	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("legacy project session not imported to %s: %v", projectPath, err)
	}
	if !strings.Contains(string(projectData), "hello from old project session") {
		t.Fatalf("legacy project session missing content:\n%s", projectData)
	}
}

// isolateConfigHome redirects os.UserConfigDir() (and the cache subtree under
// it) at a per-test temp dir by overriding the env vars Go's stdlib reads —
// HOME on darwin, XDG_CONFIG_HOME on linux. Without this, Build's plugin path
// would persist startup stats and cached schemas into the developer's real
// ~/Library/Application Support tree and bleed state across tests. Mirrors the
// withTempCache helper in internal/plugin/stats_test.go.
func isolateConfigHome(t *testing.T) string {
	t.Helper()
	dir := robustTempDir(t)
	appData := filepath.Join(dir, "AppData", "Roaming")
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("MADDOG_HOME", filepath.Join(dir, ".maddog"))
	t.Setenv("APPDATA", appData)
	t.Setenv("AppData", appData)
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
	t.Setenv("MADDOG_CREDENTIALS_STORE", "file")
	return dir
}

// TestPartitionByTier pins the bucket assignment contract that the rest of
// boot.go's plugin orchestration depends on: eager keeps its blocking startup
// slice, while empty, background, legacy lazy, and unknown tiers all warm up in
// the background.
func TestPartitionByTier(t *testing.T) {
	entries := []config.PluginEntry{
		{Name: "e1", Tier: "eager"},
		{Name: "l1", Tier: "lazy"},
		{Name: "b1", Tier: "background"},
		{Name: "default", Tier: ""}, // empty defaults to background
	}

	eager, bg := partitionByTier(entries)

	if len(eager) != 1 || eager[0].Name != "e1" {
		t.Fatalf("eager bucket = %+v, want [e1]", eager)
	}
	if len(bg) != 3 || bg[0].Name != "l1" || bg[1].Name != "b1" || bg[2].Name != "default" {
		t.Fatalf("background bucket = %+v, want [l1, b1, default] preserving input order", bg)
	}
}

func TestPluginSpecsTrustKnownCodeGraphReadTools(t *testing.T) {
	specs := PluginSpecs([]config.PluginEntry{{Name: "codegraph"}})
	if len(specs) != 1 {
		t.Fatalf("PluginSpecs returned %d specs, want 1", len(specs))
	}
	for _, name := range []string{"codegraph_context", "codegraph_search", "context", "search"} {
		if !specs[0].ReadOnlyToolNames[name] {
			t.Fatalf("codegraph spec missing read-only override for %q: %+v", name, specs[0].ReadOnlyToolNames)
		}
	}
}

func TestPluginSpecsTrustConfiguredReadOnlyTools(t *testing.T) {
	specs := PluginSpecs([]config.PluginEntry{{
		Name:                 "github",
		TrustedReadOnlyTools: []string{"issue_read", " pull_request_read ", ""},
	}})
	if len(specs) != 1 {
		t.Fatalf("PluginSpecs returned %d specs, want 1", len(specs))
	}
	for _, name := range []string{"issue_read", "pull_request_read"} {
		if !specs[0].ReadOnlyToolNames[name] {
			t.Fatalf("configured trusted read-only tool %q missing: %+v", name, specs[0].ReadOnlyToolNames)
		}
	}
	if specs[0].ReadOnlyToolNames[""] {
		t.Fatalf("empty trusted read-only tool name should be ignored: %+v", specs[0].ReadOnlyToolNames)
	}
}

func TestRememberMCPReadOnlyTrustIssuesCapabilityReceipt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MADDOG_HOME", filepath.Join(home, ".maddog"))
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, config.ProjectConfigFilename), []byte("[[plugins]]\nname = \"github\"\ncommand = \"github-mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := agent.PlanModeReadOnlyTrustRequest{
		ServerName:            "github",
		RawToolName:           "issue/read",
		IdentityFingerprint:   "identity-fingerprint",
		CapabilityFingerprint: "capability-fingerprint",
	}
	if got := rememberMCPReadOnlyTrust(root, req); got.Err != nil || !got.Saved {
		t.Fatalf("rememberMCPReadOnlyTrust = %+v, want saved receipt", got)
	}
	store := mcptrust.NewFileStore(filepath.Join(config.MaddogHomeDir(), "mcp-trust", "receipts.json"))
	if _, err := store.Get(context.Background(), req.IdentityFingerprint, req.CapabilityFingerprint); err != nil {
		t.Fatalf("capability receipt missing after persistent approval: %v", err)
	}
}

func TestRememberMCPReadOnlyTrustRejectsMissingSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MADDOG_HOME", filepath.Join(home, ".maddog"))
	root := t.TempDir()
	path := filepath.Join(root, config.ProjectConfigFilename)
	original := []byte("[[plugins]]\nname = \"github\"\ncommand = \"github-mcp\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	got := rememberMCPReadOnlyTrust(root, agent.PlanModeReadOnlyTrustRequest{ServerName: "github", RawToolName: "issue/read"})
	if got.Err == nil {
		t.Fatal("missing MCP snapshot persisted trust")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("missing MCP snapshot changed trusted_read_only_tools config")
	}
}

func TestPluginSpecsMapConfiguredCallTimeouts(t *testing.T) {
	specs := PluginSpecsForRootWithOptions([]config.PluginEntry{{
		Name:               "maker",
		Command:            "maker-mcp",
		CallTimeoutSeconds: 600,
		ToolTimeoutSeconds: map[string]int{
			"generate_video": 1800,
			" ":              120,
			"zero":           0,
		},
	}}, "", PluginSpecOptions{DefaultCallTimeout: 300 * time.Second})
	if len(specs) != 1 {
		t.Fatalf("PluginSpecs returned %d specs, want 1", len(specs))
	}
	if specs[0].DefaultCallTimeout != 5*time.Minute {
		t.Fatalf("DefaultCallTimeout = %v, want 5m", specs[0].DefaultCallTimeout)
	}
	if specs[0].CallTimeout != 10*time.Minute {
		t.Fatalf("CallTimeout = %v, want 10m", specs[0].CallTimeout)
	}
	if specs[0].ToolTimeouts["generate_video"] != 30*time.Minute {
		t.Fatalf("generate_video timeout = %v, want 30m", specs[0].ToolTimeouts["generate_video"])
	}
	if _, ok := specs[0].ToolTimeouts["zero"]; ok {
		t.Fatalf("zero tool timeout should be ignored: %+v", specs[0].ToolTimeouts)
	}
	if _, ok := specs[0].ToolTimeouts[""]; ok {
		t.Fatalf("empty tool timeout should be ignored: %+v", specs[0].ToolTimeouts)
	}
}

func TestApplyDefaultMCPCallTimeoutPreservesConfiguredDefault(t *testing.T) {
	specs := applyDefaultMCPCallTimeout([]plugin.Spec{
		{Name: "configured", DefaultCallTimeout: 2 * time.Minute},
		{Name: "empty"},
	}, 5*time.Minute)
	if specs[0].DefaultCallTimeout != 2*time.Minute {
		t.Fatalf("configured DefaultCallTimeout overwritten: %v", specs[0].DefaultCallTimeout)
	}
	if specs[1].DefaultCallTimeout != 5*time.Minute {
		t.Fatalf("empty DefaultCallTimeout = %v, want 5m", specs[1].DefaultCallTimeout)
	}
}

func TestPluginSpecsTrustPlanModeAllowedMCPTools(t *testing.T) {
	specs := PluginSpecsForRootWithPlanModeAllowedTools(
		[]config.PluginEntry{{Name: "github"}, {Name: "linear"}},
		"",
		[]string{
			"mcp__github__issue_read",
			"mcp__linear__issue_read",
			"mcp__github__",
			"read_file",
			"mcp__other__issue_read",
		},
	)
	if len(specs) != 2 {
		t.Fatalf("PluginSpecsForRootWithPlanModeAllowedTools returned %d specs, want 2", len(specs))
	}
	if !specs[0].ReadOnlyModelToolNames["mcp__github__issue_read"] {
		t.Fatalf("github allowed MCP tool missing from model trust map: %+v", specs[0].ReadOnlyModelToolNames)
	}
	if specs[0].ReadOnlyModelToolNames["mcp__github__"] || specs[0].ReadOnlyModelToolNames["mcp__other__issue_read"] {
		t.Fatalf("github trust map accepted non-concrete or other-server tools: %+v", specs[0].ReadOnlyModelToolNames)
	}
	if !specs[1].ReadOnlyModelToolNames["mcp__linear__issue_read"] {
		t.Fatalf("linear allowed MCP tool missing from model trust map: %+v", specs[1].ReadOnlyModelToolNames)
	}
}

func TestPluginSpecsForRootPinsCodeGraphToWorkspace(t *testing.T) {
	specs := PluginSpecsForRoot([]config.PluginEntry{{Name: "codegraph"}}, "/workspace")
	if len(specs) != 1 {
		t.Fatalf("PluginSpecsForRoot returned %d specs, want 1", len(specs))
	}
	if specs[0].Dir != "/workspace" {
		t.Fatalf("codegraph Dir = %q, want workspace root", specs[0].Dir)
	}
}

func TestPluginSpecsForRootDoesNotPinHTTPCodeGraph(t *testing.T) {
	specs := PluginSpecsForRoot([]config.PluginEntry{{Name: "codegraph", Type: "http", URL: "https://example.com/mcp"}}, "/workspace")
	if len(specs) != 1 {
		t.Fatalf("PluginSpecsForRoot returned %d specs, want 1", len(specs))
	}
	if specs[0].Dir != "" {
		t.Fatalf("http codegraph Dir = %q, want empty", specs[0].Dir)
	}
}

func TestPluginSpecsDoNotTrustCodeGraphToolsForOtherServers(t *testing.T) {
	specs := PluginSpecs([]config.PluginEntry{{Name: "not-codegraph"}})
	if len(specs) != 1 {
		t.Fatalf("PluginSpecs returned %d specs, want 1", len(specs))
	}
	if specs[0].ReadOnlyToolNames["codegraph_context"] {
		t.Fatalf("non-codegraph spec should not receive codegraph read-only overrides: %+v", specs[0].ReadOnlyToolNames)
	}
}

func TestBuildMigratesLegacyEagerTierToBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"

[[plugins]]
name = "legacy-eager"
command = "maddog-missing-legacy-eager-mcp"
tier = "eager"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "legacy-eager", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "legacy-eager" {
		t.Fatalf("failures = %+v, want background startup failure for migrated legacy eager plugin", failures)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "maddog.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\ntier") {
		t.Fatalf("legacy eager tier should be removed during load:\n%s", raw)
	}
}

func TestBuildMigratesLegacyLazyTierToBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"

[[plugins]]
name = "legacy-lazy"
command = "maddog-missing-legacy-lazy-mcp"
tier = "lazy"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "legacy-lazy", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "legacy-lazy" {
		t.Fatalf("failures = %+v, want background startup failure for migrated legacy lazy plugin", failures)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "maddog.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\ntier") {
		t.Fatalf("legacy lazy tier should be removed during load:\n%s", raw)
	}
}

func TestBuildColdCodegraphStartsInBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	launcher := writeCodegraphHelper(t, dir)
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q
tier = "background"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`, launcher))

	var notices []event.Event
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.Host().Failures(); len(got) != 0 {
		t.Fatalf("Host.Failures() = %+v, want empty for cold built-in codegraph background startup", got)
	}
	codegraphDir := filepath.Join(dir, ".codegraph")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(codegraphDir); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("cold codegraph init did not create .codegraph/: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	foundNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "preparing code-intelligence tools in the background") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("missing background warmup notice; got %+v", notices)
	}
}

func TestBuildCodegraphSetsShortDaemonIdleTimeout(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	launcher := writeCodegraphHelper(t, dir)
	envOut := filepath.Join(dir, "codegraph-idle-env")
	t.Setenv("MADDOG_CODEGRAPH_HELPER_ENV_OUT", envOut)

	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`, launcher))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	deadline := time.Now().Add(5 * time.Second)
	var got []byte
	for {
		got, err = os.ReadFile(envOut)
		if err == nil && string(got) == codegraph.MaddogDaemonIdleTimeoutMS {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("codegraph helper idle timeout env = %q, want %q (read error: %v)", got, codegraph.MaddogDaemonIdleTimeoutMS, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBuildWarmCodegraphIgnoresLegacyEagerTier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake launcher is a POSIX-sh script")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "slow-codegraph")
	writeFile(t, dir, "slow-codegraph", "#!/bin/sh\nif [ \"$1\" = serve ]; then sleep 5; fi\n")
	if err := os.Chmod(launcher, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "maddog.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q
tier = "eager"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`, launcher))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build should not block on warm codegraph with legacy eager tier: %v", err)
	}
	defer ctrl.Close()
}

func TestBuildDefaultsToNearestGitRoot(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "maddog.toml", `
default_model = "root-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "root-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"
`)
	t.Chdir(subdir)

	ctrl, err := Build(context.Background(), Options{Model: "root-model"})
	if err != nil {
		t.Fatalf("Build should load config from nearest git root: %v", err)
	}
	defer ctrl.Close()
}

func TestBuildMigratesLegacyEagerBeforeStatsDemotion(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	// Three samples above 2*budget — the rule in stats.go's Recommend triggers
	// when the trailing window is entirely over the threshold. Use 30s so even
	// future budget bumps stay below the threshold.
	for i := 0; i < 3; i++ {
		if err := plugin.RecordStartup("slowserver", 30*time.Second); err != nil {
			t.Fatalf("RecordStartup #%d: %v", i, err)
		}
	}

	writeFile(t, dir, "maddog.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "MADDOG_TEST_KEY_UNSET"

[[plugins]]
name = "slowserver"
command = "maddog-missing-slow-mcp-binary"
tier = "eager"
`)

	var notices []event.Event
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "slowserver", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "slowserver" {
		t.Fatalf("Host.Failures() = %+v, want background startup failure for migrated plugin", failures)
	}

	foundDemoteNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "lazy") {
			foundDemoteNotice = true
			break
		}
	}
	if foundDemoteNotice {
		t.Fatalf("demotion notice should not mention legacy lazy tier; got notices %+v", notices)
	}
}

func waitForMCPFailure(t *testing.T, h *plugin.Host, name string, timeout time.Duration) []plugin.Failure {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		failures := h.Failures()
		for _, f := range failures {
			if f.Name == name {
				return failures
			}
		}
		if time.Now().After(deadline) {
			return failures
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHelperProcess is invoked as a subprocess by TestBuildEagerStartsAtBoot
// and TestBuildLazyDoesNotConnectAtBoot. It mirrors the minimal MCP stdio
// server in internal/plugin/plugin_test.go so the boot package can drive an
// end-to-end handshake without depending on the plugin package's test helper
// (Go's testing framework only re-invokes the binary of the test package
// currently running). The helper gates on GO_WANT_HELPER_PROCESS=1 so a
// normal `go test ./internal/boot/...` does not trip it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

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
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification: no response
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "mock", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo back the message.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"msg": map[string]any{"type": "string"}},
					"required":   []string{"msg"},
				},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}
}

func writeCodegraphHelper(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "codegraph-helper")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	src := filepath.Join(dir, "codegraph-helper.go")
	if err := os.WriteFile(src, []byte(`package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "init" {
		_ = os.MkdirAll(filepath.Join(os.Args[2], ".codegraph"), 0o755)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		if out := os.Getenv("MADDOG_CODEGRAPH_HELPER_ENV_OUT"); out != "" {
			_ = os.WriteFile(out, []byte(os.Getenv("CODEGRAPH_DAEMON_IDLE_TIMEOUT_MS")), 0o644)
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
			ID     *int            `+"`json:\"id\"`"+`
			Method string          `+"`json:\"method\"`"+`
			Params json.RawMessage `+"`json:\"params\"`"+`
		}
		if err := json.Unmarshal(line, &req); err != nil || req.ID == nil {
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "codegraph", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "search",
				"description": "Search symbols.",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", path, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build codegraph helper: %v\n%s", err, out)
	}
	return path
}
