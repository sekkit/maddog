package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"maddog/internal/contextpack"
	"maddog/internal/diff"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/instruction"
	"maddog/internal/jobs"
	"maddog/internal/memory"
	"maddog/internal/memorycompiler"
	"maddog/internal/nilutil"
	"maddog/internal/planmode"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

// maxToolOutputBytes caps a single tool result before it goes into the model's
// context. ~32KB is roughly 8K tokens — enough for a full file read or a busy
// grep, while preventing one accidental "read this 5 MB log" from blowing the
// window before the next compaction runs.
const maxToolOutputBytes = 32 * 1024

const maxFinalReadinessBlocks = 3
const maxEmptyFinalBlocks = 3
const maxStreamRecoveries = 3
const maxExecutorHandoffNudges = 1
const memoryCompilerInjectionMax = 5
const memoryCompilerInjectionCooldown = 30 * time.Second

// Renderer redraws the assistant's final-answer text as styled output. It is
// applied only after a turn's text stream completes, so the user sees raw
// markdown stream live, then a single redraw replaces it with formatted
// output. The renderer is intentionally interface-shaped so the agent stays
// independent of the cli's markdown library choice. Consumed by TextSink.
type Renderer interface {
	Render(text string) string
}

// Asker puts structured multiple-choice questions to the user and blocks for the
// answers. The agent consults it for the `ask` tool. It is interface-shaped so
// the agent stays independent of the frontend; a nil asker means no interactive
// user (headless runs), where `ask` returns a "decide for yourself" result. The
// interactive frontends wire the controller in as the Asker.
type Asker interface {
	Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)
}

// callContextKey carries the executing tool call's identity into Execute.
type callContextKey struct{}
type parentSessionContextKey struct{}
type userImagesContextKey struct{}

// callContext is the per-call context a tool can read. parentID is the call being
// executed and sink is the agent's event sink (the `task` tool uses both to nest
// a sub-agent's events under this call); asker lets the `ask` tool reach the user.
type callContext struct {
	parentID string
	sink     event.Sink
	asker    Asker
	planMode bool
}

// withCallContext stamps ctx with the executing call's ID, the agent's sink, and
// the asker. executeOne sets this before every Execute; `task` reads it (via
// CallContext) to nest sub-agent events, and `ask` reads the asker to prompt.
func withCallContext(ctx context.Context, parentID string, sink event.Sink, asker Asker, planMode bool) context.Context {
	return context.WithValue(ctx, callContextKey{}, callContext{parentID: parentID, sink: sink, asker: asker, planMode: planMode})
}

// CallContext returns the executing call's ID, the agent's sink, and the asker,
// if the context was set by an agent's executeOne. ok is false for a plain
// context (headless tool tests, calls made outside the run loop).
func CallContext(ctx context.Context) (parentID string, sink event.Sink, asker Asker, ok bool) {
	cc, ok := ctx.Value(callContextKey{}).(callContext)
	if !ok {
		return "", nil, nil, false
	}
	return cc.parentID, cc.sink, cc.asker, true
}

// PlanModeFromContext reports whether the tool call is executing under the
// agent's read-only planning gate. Tools that are themselves ReadOnly may use
// this to avoid enabling follow-up writer-only surfaces during planning.
func PlanModeFromContext(ctx context.Context) bool {
	cc, ok := ctx.Value(callContextKey{}).(callContext)
	return ok && cc.planMode
}

// WithParentSession stamps the active parent session ID onto a turn context so
// persisted sub-agents can record and enforce their owning conversation.
func WithParentSession(ctx context.Context, parentSession string) context.Context {
	return context.WithValue(ctx, parentSessionContextKey{}, strings.TrimSpace(parentSession))
}

// ParentSession returns the active parent session ID carried by a turn context.
func ParentSession(ctx context.Context) string {
	parentSession, _ := ctx.Value(parentSessionContextKey{}).(string)
	return strings.TrimSpace(parentSession)
}

// WithUserImages carries the data URLs of images the user attached to this turn,
// resolved by the controller (which owns attachments) since the agent must not
// depend on it. Run embeds them on the user message; the provider sends them only
// when the model is vision-capable.
func WithUserImages(ctx context.Context, images []string) context.Context {
	return context.WithValue(ctx, userImagesContextKey{}, images)
}

func userImages(ctx context.Context) []string {
	images, _ := ctx.Value(userImagesContextKey{}).([]string)
	return images
}

// Gate decides, per tool call, whether it may run. The agent consults it at
// execute time (after the plan-mode gate). It is interface-shaped so the agent
// stays independent of the permission package and of how "ask" is resolved
// (silently in headless runs, interactively in the chat TUI). A nil gate means
// no gating — every call runs, preserving behaviour for callers that don't wire
// one in. reason is fed back to the model when allow is false; a non-nil err
// (e.g. ctx cancelled awaiting approval) is treated as a block for that call.
type Gate interface {
	Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (allow bool, reason string, err error)
}

// PlanModeReadOnlyTrustRequest describes an external read-only hint that plan
// mode will not trust without a user decision. ToolName is the provider-visible
// name; ServerName and RawToolName are the MCP identifiers persisted in config.
type PlanModeReadOnlyTrustRequest struct {
	ToolName    string
	ServerName  string
	RawToolName string
	Args        json.RawMessage
}

// PlanModeReadOnlyTrustGate optionally confirms an MCP server's self-reported
// read-only hint at execution time. It is separate from Gate because the
// plan-mode check runs before ordinary permission policy.
type PlanModeReadOnlyTrustGate interface {
	CheckPlanModeReadOnlyTrust(ctx context.Context, req PlanModeReadOnlyTrustRequest) (allow bool, reason string, err error)
}

// ToolHooks fires user-configured shell hooks around each tool call. PreToolUse
// runs before the call and may block it (block=true; message is the reason fed
// back to the model); PostToolUse runs after and only surfaces output to the
// user (it can't block). It is interface-shaped so the agent stays independent
// of the hook package — a nil hooks field disables hook firing entirely.
type ToolHooks interface {
	PreToolUse(ctx context.Context, name string, args json.RawMessage) (block bool, message string)
	PostToolUse(ctx context.Context, name string, args json.RawMessage, result string)
	// PostLLMCall fires after each model turn completes (streaming finishes)
	// but before reasoning_content is stored. It returns the (possibly
	// translated) reasoning string — the original when no hook is configured.
	// HasPostLLMCall reports whether such a hook exists, so the agent keeps
	// streaming reasoning live when none is wired up.
	PostLLMCall(ctx context.Context, reasoning string, turn int) string
	HasPostLLMCall() bool
	// SubagentStop fires when a `task` sub-agent finishes (foreground). PreCompact
	// fires just before a compaction pass and returns extra summary guidance (its
	// hooks' stdout) to fold into the summary prompt; "" when no hook contributes.
	SubagentStop(ctx context.Context, last string)
	PreCompact(ctx context.Context, trigger string) string
}

// Agent drives a single task: a Provider, a tool Registry, and a Session wired
// into the main loop.
type Agent struct {
	prov             provider.Provider
	tools            *tool.Registry
	session          *Session
	sessMu           sync.Mutex // guards the session pointer for external Session()/SetSession
	maxSteps         int
	maxStepsKey      string
	maxParallelTools int
	// executorHandoffGuard is enabled by Coordinator for the executor agent. The
	// per-turn marker check in Run keeps ordinary single-model turns unaffected.
	executorHandoffGuard bool
	temperature          float64
	pricing              *provider.Pricing
	pxpipeSummary        *event.PxpipeSummary
	reasoningLanguage    atomic.Value // string: auto|zh|en
	responseLanguage     atomic.Value // string: auto|zh|en

	upgradePolicy         UpgradePolicy
	frontierProv          provider.Provider
	frontierPricing       *provider.Pricing
	frontierContextWindow int
	frontierTarget        string
	usageRole             string
	usageModel            string
	usageEffort           string
	defaultProv           provider.Provider
	defaultPricing        *provider.Pricing
	defaultContextWindow  int
	upgraded              bool
	onFrontier            bool
	frontierReceiptStart  int
	frontierTokens        atomic.Int64
	advisor               AdvisorConfig
	advisorRunner         AdvisorRunner
	nativeAdvisor         *provider.NativeAdvisorConfig
	advisorTurnUses       int
	advisorSessionUses    int

	// sink receives the turn's typed event stream (reasoning/text deltas, tool
	// dispatch/results, usage, notices). The agent no longer formats output
	// itself — a frontend's Sink decides how to render. Never nil; New defaults
	// it to event.Discard.
	sink event.Sink

	// lastUsage caches the most recent per-turn telemetry the provider reported so
	// the CLI can expose a context gauge without re-scraping the usage line. The
	// run loop writes it while a frontend's status line reads it, so it is atomic.
	lastUsage atomic.Pointer[provider.Usage]

	// sessCacheHit/sessCacheMiss accumulate cache tokens across every API call
	// this session, so frontends can show the aggregate hit-rate (Σhit/Σ(hit+miss))
	// — a steadier, cost-oriented number than the single-turn rate. They are NOT
	// reset on compaction (compaction only rewrites session.Messages), so the
	// aggregate never craters when the prefix is summarized away. Atomic: the run
	// loop accumulates them while the status line reads them.
	sessCacheHit  atomic.Int64
	sessCacheMiss atomic.Int64

	// lastPrefixShape records the previous provider request's cacheable prefix
	// so usage events can explain prefix churn on the next request.
	lastPrefixShape     PrefixShape
	haveLastPrefixShape bool

	// planMode, when true, refuses any tool call whose ReadOnly() is false.
	// The system prompt and tool list never change with the toggle so the
	// prompt-cache prefix stays valid; the gating happens at execute time
	// and the model sees a "blocked" result it can adapt to. Toggled from
	// the outside via SetPlanMode.
	planMode atomic.Bool

	// gate, when non-nil, is the per-call permission gate consulted after the
	// plan-mode check. nil disables gating entirely.
	gate Gate

	// planModeReadOnlyTrust, when non-nil, can ask the user to trust an MCP
	// server's readOnlyHint for plan-mode execution without changing tool schemas.
	planModeReadOnlyTrust PlanModeReadOnlyTrustGate

	// hooks, when non-nil, fires PreToolUse / PostToolUse shell hooks around each
	// tool call. nil disables hook firing.
	hooks ToolHooks

	// asker, when non-nil, lets the `ask` tool put questions to the user. nil in
	// headless runs (no interactive user). Set via SetAsker.
	asker Asker

	// onPreEdit, when non-nil, is called with a writer tool's previewed change
	// just before it runs — the seam the checkpoint store uses to snapshot a
	// file's pre-edit content. Only fires for non-ReadOnly tools that implement
	// tool.Previewer (so bash, whose targets are unknowable, is never tracked).
	// Set via SetPreEditHook.
	onPreEdit func(diff.Change)

	// jobs, when non-nil, is the session's background-job manager. executeOne
	// stamps it onto each tool call's context so the background tools (bash
	// run_in_background, task run_in_background, bash_output/kill_shell/wait) can
	// reach it. nil leaves those tools to degrade gracefully.
	jobs *jobs.Manager

	// toolOutputCompressor optionally reduces high-volume tool results before
	// they enter session history and the next model request. Full raw outputs are
	// retained only when compression actually saves context.
	toolOutputCompressor  contextpack.ToolOutputCompressor
	toolOutputCompression contextpack.Options
	rawToolResultDir      string
	rawToolResultsMu      sync.RWMutex
	rawToolResults        map[string]string
	toolCompressionsMu    sync.Mutex
	toolCompressions      []ToolCompressionRecord

	// steerQueue holds mid-turn user messages queued while the agent is
	// running. Each is consumed once per loop iteration, persisted to the
	// session for history replay, and sent to the model as guidance (not a
	// new task). Cache miss for the next API call is unavoidable but limited
	// to one call — the prefix stays stable otherwise.
	steerMu       sync.Mutex
	steerQueue    []string
	steerConsumed bool

	// evidence is a per-user-turn ledger of host-observed tool receipts. It lets
	// complete_step validate that cited evidence happened before the claim.
	evidence *evidence.Ledger

	// pendingControlSignals are host/controller observations that should seed the
	// next run's routing ledger after the per-turn reset.
	pendingControlMu      sync.Mutex
	pendingControlSignals []evidence.FailureSignal

	// todoState is the host's canonical task list: the latest successful
	// todo_write with completions applied by complete_step. Unlike the per-turn
	// ledger it survives turn boundaries and compaction (it never rides in the
	// prompt), so the final-answer gate still sees an unfinished plan a later
	// turn would otherwise hide. Rebuilt from the session in SetSession.
	todoMu    sync.Mutex
	todoState []evidence.TodoItem

	// hostAdvanceSeq guarantees unique tool IDs across turns: every
	// emitTodoState call increments it so the frontend always sees a fresh
	// dispatch even when the same panel index is signed off in different turns.
	hostAdvanceSeq atomic.Int64

	// projectChecks are structured project instructions that complete_step can
	// verify against same-turn bash receipts after a write-backed completion.
	projectChecks []instruction.VerifyCheck

	// memQueue, when non-nil, lets the remember/forget tools fold a turn-tail note
	// about a just-made memory change into the next turn, so it applies this
	// session without touching the cache-stable prefix. Set via SetMemoryQueue.
	memQueue memory.Queue

	// memoryCompiler, when non-nil, records execution traces and may compile the
	// user turn into a compact execution contract. It never mutates the stable
	// system prompt or tool schema.
	memoryCompilerMu sync.RWMutex
	memoryCompiler   *memorycompiler.Runtime
	compilerTurn     *memorycompiler.Turn

	// compilerInjectionMu bounds how often Memory v5 may replace a visible user
	// turn with an execution contract. The runtime can still observe throttled
	// turns for trace writeback, but prompt injection and UI citations stay
	// limited so the compiler does not dominate every conversation turn.
	compilerInjectionMu    sync.Mutex
	lastCompilerInjectedAt time.Time
	compilerInjectionCount int

	// classifier 用于判断用户输入是任务还是聊天，决定是否启动 Memory v5
	classifier TaskClassifier

	// planModeAllowedTools declares extra custom tools that the centralized
	// plan-mode policy may treat as read-only. Known blocked tools still lose.
	// Populated from Options.PlanModeAllowedTools during construction.
	planModeAllowedTools []string

	// Context management: when a turn's prompt nears contextWindow, the older
	// middle of the session is summarized away, keeping a token-bounded recent
	// tail verbatim (recentKeep is the message floor) and archiving the originals
	// under archiveDir. compactStuck latches when compaction can't get the prompt
	// under the window (consecutiveCompacts crosses the limit), so auto-compaction
	// pauses instead of looping. softCompactNoticed gates the one-shot soft-ratio
	// notice so it fires once per approach, not every turn.
	contextWindow       int
	softCompactRatio    float64
	toolResultSnipRatio float64
	compactRatio        float64
	compactForceRatio   float64
	softCompactNoticed  bool
	recentKeep          int
	archiveDir          string
	keepPolicy          KeepPolicy
	compactStuck        bool
	consecutiveCompacts int

	// stormSig / stormCount track a run of turns that keep failing the same way so
	// the loop can break a death-spiral. The signature is each call's (tool, error)
	// in order, NOT (tool, args): a stuck model reliably reworks the arguments
	// cosmetically (a re-worded essay, a reordered object) while the call fails
	// identically every time — keying on args misses the loop entirely (observed
	// live against truncated tool-call arguments). Because errors that embed their
	// subject (e.g. "file not found: /x") differ per target, genuine varied probing
	// does not collapse to one signature. Reset whenever a turn does anything else
	// (a different failure shape, or any success). See applyStormBreaker.
	stormSig   string
	stormCount int

	// repeatSuccessCounts tracks write-like tool calls that have already
	// succeeded in this user turn. This catches the complementary loop shape to
	// stormSig: a model keeps doing the same successful write, so there is no
	// error for the failure-only storm breaker to see.
	repeatSuccessCounts map[string]int
}

type ToolCompressionRecord struct {
	ToolName    string
	Compression event.Compression
}

// KeepPolicy is a bitmask controlling which messages are preserved beyond the
// recent tail during compaction.
type KeepPolicy int

const (
	KeepErrors KeepPolicy = 1 << iota
	KeepUserMarked
)

// SetPlanMode flips the read-only gate. While true, executeOne refuses any
// non-ReadOnly tool the model calls and returns a "blocked" result instead of
// running it. The cache-friendly bits — system prompt, tools schema, message
// history — are left untouched, so the toggle costs nothing in cache hits.
func (a *Agent) SetPlanMode(v bool) { a.planMode.Store(v) }

// SetReasoningLanguage updates the visible reasoning language preference for
// subsequent user-role messages emitted by this agent.
func (a *Agent) SetReasoningLanguage(lang string) {
	if a == nil {
		return
	}
	a.reasoningLanguage.Store(NormalizeReasoningLanguage(lang))
}

// SetResponseLanguage updates the final-answer language preference for
// subsequent user-role messages emitted by this agent.
func (a *Agent) SetResponseLanguage(lang string) {
	if a == nil {
		return
	}
	a.responseLanguage.Store(NormalizeResponseLanguage(lang))
}

// SetGate installs the per-call permission gate. Used by `maddog chat` to swap the
// headless gate built in setup for an interactive one that prompts the user;
// nil disables gating. Safe to call before the run loop starts.
func (a *Agent) SetGate(g Gate) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.gate = g
}

// SetPlanModeReadOnlyTrustGate installs the optional confirmation path for MCP
// tools whose read-only flag comes from an external readOnlyHint.
func (a *Agent) SetPlanModeReadOnlyTrustGate(g PlanModeReadOnlyTrustGate) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.planModeReadOnlyTrust = g
}

func (a *Agent) withTurnPreferences(input string) string {
	if a == nil {
		return input
	}
	responseLang := "auto"
	if v := a.responseLanguage.Load(); v != nil {
		if s, ok := v.(string); ok {
			responseLang = s
		}
	}
	input = WithResponseLanguage(input, responseLang)

	lang := "auto"
	if v := a.reasoningLanguage.Load(); v != nil {
		if s, ok := v.(string); ok {
			lang = s
		}
	}
	return WithReasoningLanguage(input, lang)
}

// SetAsker installs the asker the `ask` tool uses to question the user.
// Interactive frontends wire one in; headless runs leave it nil.
func (a *Agent) SetAsker(as Asker) { a.asker = as }

// SetMemoryQueue installs the sink the remember/forget tools use to apply a
// memory change in the current session. The controller wires itself in.
func (a *Agent) SetMemoryQueue(q memory.Queue) { a.memQueue = q }

// SetPreEditHook installs the pre-edit snapshot hook (see onPreEdit). The
// controller wires it to its per-session checkpoint store; nil disables capture.
func (a *Agent) SetPreEditHook(fn func(diff.Change)) { a.onPreEdit = fn }

// Session returns the agent's current conversation, useful for persistence
// hooks that need to read the message log between turns. sessMu serialises this
// pointer read against SetSession, so a frontend (serve's concurrent /history and
// /new handlers) can't race the swap. The run loop touches a.session directly and
// only swaps it via SetSession while idle, so its reads need no lock.
func (a *Agent) Session() *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	return a.session
}

// EvidenceReceipts returns a copy of the current turn's host evidence ledger.
func (a *Agent) EvidenceReceipts() []evidence.Receipt {
	if a == nil || a.evidence == nil {
		return nil
	}
	return a.evidence.Snapshot()
}

func (a *Agent) ToolCompressions() []ToolCompressionRecord {
	if a == nil {
		return nil
	}
	a.toolCompressionsMu.Lock()
	defer a.toolCompressionsMu.Unlock()
	out := make([]ToolCompressionRecord, len(a.toolCompressions))
	copy(out, a.toolCompressions)
	return out
}

func (a *Agent) resetToolCompressions() {
	if a == nil {
		return
	}
	a.toolCompressionsMu.Lock()
	a.toolCompressions = nil
	a.toolCompressionsMu.Unlock()
}

func (a *Agent) recordToolCompression(toolName string, c *event.Compression) {
	if a == nil || c == nil {
		return
	}
	a.toolCompressionsMu.Lock()
	a.toolCompressions = append(a.toolCompressions, ToolCompressionRecord{ToolName: toolName, Compression: *c})
	a.toolCompressionsMu.Unlock()
}

// SetSession replaces the agent's conversation wholesale. Used by
// `maddog chat --resume` to load a saved JSONL transcript before the first turn,
// so the model picks up exactly where it left off. Callers serialise it against a
// running turn (it only fires while idle); sessMu guards the pointer swap itself.
func (a *Agent) SetSession(s *Session) {
	a.sessMu.Lock()
	a.session = s
	a.sessMu.Unlock()
	a.sessCacheHit.Store(0)
	a.sessCacheMiss.Store(0)
	a.clearRawToolResults()
	if s != nil {
		a.rebuildTodoState(s.Snapshot())
	}
	a.resetMemoryCompilerInjectionGate()
	// 清除分类缓存（会话边界）
	a.clearClassifierCache()
}

// RawToolResult returns the uncompressed text retained for a compressed tool
// call. Raw results are kept in memory and keyed by provider tool call ID.
func (a *Agent) RawToolResult(toolID string) (string, bool) {
	if a == nil || toolID == "" {
		return "", false
	}
	a.rawToolResultsMu.RLock()
	raw, ok := a.rawToolResults[toolID]
	a.rawToolResultsMu.RUnlock()
	if ok {
		return raw, true
	}
	if a.rawToolResultDir == "" {
		return "", false
	}
	b, err := os.ReadFile(a.rawToolResultPath(toolID))
	if err != nil {
		return "", false
	}
	raw = string(b)
	a.rawToolResultsMu.Lock()
	if a.rawToolResults == nil {
		a.rawToolResults = make(map[string]string)
	}
	a.rawToolResults[toolID] = raw
	a.rawToolResultsMu.Unlock()
	return raw, true
}

func (a *Agent) storeRawToolResult(toolID, raw string) error {
	if a == nil || toolID == "" {
		return nil
	}
	a.rawToolResultsMu.Lock()
	if a.rawToolResults == nil {
		a.rawToolResults = make(map[string]string)
	}
	a.rawToolResults[toolID] = raw
	a.rawToolResultsMu.Unlock()
	if a.rawToolResultDir == "" {
		return nil
	}
	if err := os.MkdirAll(a.rawToolResultDir, 0o700); err != nil {
		a.clearRawToolResult(toolID)
		return err
	}
	if err := os.WriteFile(a.rawToolResultPath(toolID), []byte(raw), 0o600); err != nil {
		a.clearRawToolResult(toolID)
		return err
	}
	return nil
}

func (a *Agent) clearRawToolResult(toolID string) {
	if a == nil || toolID == "" {
		return
	}
	a.rawToolResultsMu.Lock()
	defer a.rawToolResultsMu.Unlock()
	delete(a.rawToolResults, toolID)
}

func (a *Agent) clearRawToolResults() {
	if a == nil {
		return
	}
	a.rawToolResultsMu.Lock()
	defer a.rawToolResultsMu.Unlock()
	a.rawToolResults = nil
}

func (a *Agent) SetRawToolResultDir(dir string) {
	if a == nil {
		return
	}
	a.rawToolResultsMu.Lock()
	defer a.rawToolResultsMu.Unlock()
	a.rawToolResultDir = strings.TrimSpace(dir)
	a.rawToolResults = nil
}

func (a *Agent) rawToolResultPath(toolID string) string {
	sum := sha256.Sum256([]byte(toolID))
	return filepath.Join(a.rawToolResultDir, hex.EncodeToString(sum[:])+".txt")
}

// LastUsage returns the most recent per-turn token telemetry the provider
// reported (nil if no turn has run yet). The TUI uses it to show a context
// gauge alongside the prompt; the actual cache decisions still live inside
// maybeCompact.
func (a *Agent) LastUsage() *provider.Usage { return a.lastUsage.Load() }

// SessionCache returns the cumulative cache hit/miss prompt tokens across every
// API call this session — the basis for the status line's aggregate hit-rate.
func (a *Agent) SessionCache() (hit, miss int) {
	return int(a.sessCacheHit.Load()), int(a.sessCacheMiss.Load())
}

// ContextWindow returns the configured context-window size in tokens. 0
// means compaction is disabled for this agent.
func (a *Agent) ContextWindow() int { return a.contextWindow }

// mid-turn steer marker.
// MidTurnSteerPrefix marks user messages that were injected mid-turn as
// guidance (via Steer). The model sees them as instructions; frontends
// display them as a notice, not a regular user bubble.
const MidTurnSteerPrefix = "[Mid-turn steer queued by the user. Do not treat this as a new task; use it only as additional guidance for the current task after completing the current step.]"

func midTurnSteerMessage(text string) string {
	return MidTurnSteerPrefix + "\n" + text
}

// SteerText checks whether content is a mid-turn steer message and, if so,
// returns the original user text without the wrapper prefix. The returned
// text preserves the user's exact input — it only strips the prefix and the
// "\n" separator that midTurnSteerMessage inserts between the prefix and the
// user text; it does not trim spaces so the history replay matches the live
// Steer event rendering character-for-character.
func SteerText(content string) (string, bool) {
	after, found := strings.CutPrefix(content, MidTurnSteerPrefix)
	if !found {
		return "", false
	}
	// Strip only the "\n" separator, preserving the user's original text.
	after = strings.TrimPrefix(after, "\n")
	return after, true
}

// Steer queues a message for mid-turn injection.
func (a *Agent) Steer(text string) {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	a.steerQueue = append(a.steerQueue, text)
	a.steerConsumed = false
}

// SteerConsumed returns true when the steer queue became empty after the last consume.
func (a *Agent) SteerConsumed() bool {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return a.steerConsumed
}

func (a *Agent) consumeSteer() (string, bool) {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if len(a.steerQueue) == 0 {
		return "", false
	}
	t := a.steerQueue[0]
	a.steerQueue = a.steerQueue[1:]
	a.steerConsumed = len(a.steerQueue) == 0
	return t, true
}

func (a *Agent) clearSteerQueue() {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	a.steerQueue = nil
	a.steerConsumed = false
}

func (a *Agent) steerQueueLen() int {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return len(a.steerQueue)
}

// CompactRatio returns the fraction of the window at which auto-compaction
// fires (e.g. 0.8). The status line uses it to show headroom to the next compact.
func (a *Agent) CompactRatio() float64 { return a.compactRatio }

// CompactNow runs one compaction pass immediately, regardless of the
// usage-ratio threshold maybeCompact normally honours. Used by the chat
// TUI's `/compact` command so the user can reset the prefix before it
// naturally fills up.
func (a *Agent) CompactNow(ctx context.Context, instructions string) error {
	return a.compact(ctx, "manual", instructions, true)
}

// Options configures an Agent.
type Options struct {
	MaxSteps int
	// MaxStepsKey names the configuration knob shown when the MaxSteps guard is
	// hit. Empty defaults to agent.max_steps.
	MaxStepsKey string
	Temperature float64
	Pricing     *provider.Pricing // optional, for per-turn cost display
	UsageSource string            // optional billable usage source; default executor
	// PxpipeSummary is optional content-free gateway telemetry from the safe
	// pxpipe event parser. Nil disables pxpipe reporting on Usage events.
	PxpipeSummary *event.PxpipeSummary

	// Gate is the per-call permission gate. nil disables gating.
	Gate Gate

	// PlanModeReadOnlyTrustGate confirms untrusted external read-only hints when
	// plan mode would otherwise block them. nil keeps fail-closed behavior.
	PlanModeReadOnlyTrustGate PlanModeReadOnlyTrustGate
	PlanModeAllowedTools      []string

	// Context management. ContextWindow <= 0 disables compaction. Ratios and
	// RecentKeep fall back to defaults when unset.
	ContextWindow       int
	SoftCompactRatio    float64
	ToolResultSnipRatio float64
	CompactRatio        float64
	CompactForceRatio   float64
	RecentKeep          int
	ArchiveDir          string
	KeepPolicy          KeepPolicy

	// MaxParallelTools caps how many read-only tool calls (including parallel
	// subagent dispatches) run concurrently in one batch. <= 0 = default (8).
	MaxParallelTools int

	// Hooks fires PreToolUse / PostToolUse shell hooks around tool calls. nil
	// disables hook firing.
	Hooks ToolHooks

	// Jobs is the session's background-job manager (nil disables background tools).
	Jobs *jobs.Manager

	// ToolOutputCompressor, when non-nil, compresses high-volume tool results
	// before they enter session history and the next model request.
	ToolOutputCompressor  contextpack.ToolOutputCompressor
	ToolOutputCompression contextpack.Options
	RawToolResultDir      string

	// ProjectChecks are host-observable structured checks extracted during boot.
	ProjectChecks []instruction.VerifyCheck

	// ReasoningLanguage controls visible reasoning language preference as transient
	// user-turn context. Empty/auto injects nothing.
	ReasoningLanguage string

	// MemoryCompiler enables Memory v5 execution-contract compilation for
	// user-authored task turns.
	MemoryCompiler *memorycompiler.Runtime

	// Frontier upgrade routing. When UpgradePolicy selects an upgrade, the
	// current turn continues on FrontierProvider while preserving session state.
	UpgradePolicy         UpgradePolicy
	FrontierProvider      provider.Provider
	FrontierPricing       *provider.Pricing
	FrontierContextWindow int
	FrontierTarget        string
	UsageRole             string
	UsageModel            string
	UsageEffort           string
	Advisor               AdvisorConfig
	AdvisorRunner         AdvisorRunner
	NativeAdvisor         *provider.NativeAdvisorConfig
}

// New constructs an Agent. MaxSteps <= 0 means no cap — the run loop continues
// until the model gives a final answer, the context is cancelled, or the
// provider errors (compaction keeps the context bounded). A nil sink is replaced
// with event.Discard so the agent can always emit unconditionally.
func New(prov provider.Provider, tools *tool.Registry, session *Session, opts Options, sink event.Sink) *Agent {
	if opts.SoftCompactRatio <= 0 {
		opts.SoftCompactRatio = defaultSoftCompactRatio
	}
	if opts.ToolResultSnipRatio <= 0 {
		opts.ToolResultSnipRatio = defaultToolResultSnipRatio
	}
	if opts.CompactRatio <= 0 {
		opts.CompactRatio = defaultCompactRatio
	}
	if opts.ToolResultSnipRatio >= opts.CompactRatio {
		opts.ToolResultSnipRatio = opts.CompactRatio
	}
	if opts.CompactForceRatio <= 0 {
		opts.CompactForceRatio = defaultCompactForceRatio
	}
	if opts.RecentKeep <= 0 {
		opts.RecentKeep = minRecentKeep
	}
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	gate := opts.Gate
	if nilutil.IsNil(gate) {
		gate = nil
	}
	planModeReadOnlyTrust := opts.PlanModeReadOnlyTrustGate
	if nilutil.IsNil(planModeReadOnlyTrust) {
		planModeReadOnlyTrust = nil
	}
	hooks := opts.Hooks
	if nilutil.IsNil(hooks) {
		hooks = nil
	}
	toolOutputCompressor := opts.ToolOutputCompressor
	if nilutil.IsNil(toolOutputCompressor) {
		toolOutputCompressor = nil
	}
	maxStepsKey := opts.MaxStepsKey
	if strings.TrimSpace(maxStepsKey) == "" {
		maxStepsKey = "agent.max_steps"
	}
	a := &Agent{
		prov:                  prov,
		tools:                 tools,
		session:               session,
		maxSteps:              opts.MaxSteps,
		maxStepsKey:           maxStepsKey,
		maxParallelTools:      opts.MaxParallelTools,
		temperature:           opts.Temperature,
		pricing:               opts.Pricing,
		pxpipeSummary:         clonePxpipeSummary(opts.PxpipeSummary),
		upgradePolicy:         opts.UpgradePolicy,
		frontierProv:          opts.FrontierProvider,
		frontierPricing:       opts.FrontierPricing,
		frontierContextWindow: opts.FrontierContextWindow,
		frontierTarget:        opts.FrontierTarget,
		usageRole:             strings.TrimSpace(opts.UsageRole),
		usageModel:            strings.TrimSpace(opts.UsageModel),
		usageEffort:           strings.TrimSpace(opts.UsageEffort),
		advisor:               opts.Advisor,
		advisorRunner:         opts.AdvisorRunner,
		nativeAdvisor:         cloneNativeAdvisor(opts.NativeAdvisor),
		sink:                  sink,
		gate:                  gate,
		planModeReadOnlyTrust: planModeReadOnlyTrust,
		hooks:                 hooks,
		jobs:                  opts.Jobs,
		toolOutputCompressor:  toolOutputCompressor,
		toolOutputCompression: opts.ToolOutputCompression,
		rawToolResultDir:      strings.TrimSpace(opts.RawToolResultDir),
		evidence:              evidence.NewLedger(),
		projectChecks:         append([]instruction.VerifyCheck(nil), opts.ProjectChecks...),
		memoryCompiler:        opts.MemoryCompiler,
		classifier:            newHeuristicClassifier(),
		planModeAllowedTools:  append([]string(nil), opts.PlanModeAllowedTools...),
		contextWindow:         opts.ContextWindow,
		softCompactRatio:      opts.SoftCompactRatio,
		toolResultSnipRatio:   opts.ToolResultSnipRatio,
		compactRatio:          opts.CompactRatio,
		compactForceRatio:     opts.CompactForceRatio,
		recentKeep:            opts.RecentKeep,
		keepPolicy:            opts.KeepPolicy,
		archiveDir:            opts.ArchiveDir,
	}
	if a.tools != nil {
		a.tools.Add(rawToolResultTool{agent: a})
		if a.advisorRunner != nil && a.nativeAdvisor == nil && a.advisor.MaxUsesPerTurn > 0 {
			a.tools.Add(fallbackAdvisorTool{agent: a})
		}
	}
	a.SetReasoningLanguage(opts.ReasoningLanguage)
	return a
}

func usageSourceOrDefault(source, fallback string) string {
	source = strings.TrimSpace(source)
	if source != "" {
		return source
	}
	return fallback
}

func clonePxpipeSummary(in *event.PxpipeSummary) *event.PxpipeSummary {
	if in == nil {
		return nil
	}
	out := *in
	out.Statuses = cloneIntCountMap(in.Statuses)
	out.Paths = cloneStringCountMap(in.Paths)
	out.Models = cloneStringCountMap(in.Models)
	out.Reasons = cloneStringCountMap(in.Reasons)
	return &out
}

func cloneStringCountMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntCountMap(in map[int]int) map[int]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Run appends the user input and drives the tool loop until the model returns a
// final answer (no tool calls), the context is cancelled, or the provider errors.
// With maxSteps <= 0 the loop is unbounded — the natural termination is the model
// finishing, and the real safety bounds are user cancellation and compaction, not
// a round count. A positive maxSteps imposes an optional hard guard, surfaced as
// a resumable notice when hit.
func (a *Agent) Run(ctx context.Context, input string) (runErr error) {
	defer a.clearSteerQueue()
	a.steerMu.Lock()
	a.steerConsumed = false
	a.steerMu.Unlock()
	a.resetRoutingForTurn()
	if a.evidence != nil {
		a.evidence.Reset()
		a.applyPendingControlSignals()
	}
	a.resetToolCompressions()
	a.repeatSuccessCounts = nil
	a.sink.Emit(event.Event{Kind: event.TurnStarted})
	rawInput := input
	memoryCompilerInput := rawInput
	if sourceInput, ok := MemoryCompilerSourceInputFromContext(ctx); ok {
		memoryCompilerInput = sourceInput
	}
	input = a.withTurnPreferences(rawInput)
	if memCompiler := a.memoryCompilerRuntime(); memCompiler != nil && !MemoryCompilerSkipFromContext(ctx) && shouldStartMemoryCompiler(memoryCompilerInput) {
		// 使用分类器判断是否为任务
		isTask := true // 默认为任务
		var classifyErr error
		if a.classifier != nil {
			isTask, classifyErr = a.classifier.IsTask(ctx, memoryCompilerInput)
			if classifyErr != nil {
				// 分类失败时降级到启发式分类器
				isTask = shouldInjectMemoryCompilerContractForInput(memoryCompilerInput)
			}
		}

		// 只有任务才启动 Memory v5
		if isTask {
			if compiledInput, turn := memCompiler.StartTurn(ctx, memoryCompilerInput, a.session.Snapshot()); turn != nil {
				injected := strings.TrimSpace(compiledInput) != "" &&
					a.tryMarkMemoryCompilerInjected(time.Now())
				if !injected {
					turn.SuppressInjection()
				}
				a.compilerTurn = turn
				a.emitMemoryCompilerStats(turn)
				defer func() {
					turn.Finish(runErr)
					if a.compilerTurn == turn {
						a.compilerTurn = nil
					}
				}()
				if injected {
					input = a.withTurnPreferences(compiledInput)
				}
			}
		}
	}
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: input, Images: userImages(ctx)})
	a.evaluateInitialRouting(ctx)

	finalReadinessBlocks := 0
	emptyFinalBlocks := 0
	handoffNudges := 0
	usedAnyTool := false
	streamRecoveries := 0
	graceRound := false
	executorHandoff := a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker)
	for step := 0; a.maxSteps <= 0 || step < a.maxSteps || graceRound; step++ {
		// Consume a queued steer and persist it to the session so it
		// survives tab switches and history replay. The model sees it as
		// guidance (with a prefix), not a new task. One cache miss per
		// steer is unavoidable — the model must see the new instruction.
		if text, ok := a.consumeSteer(); ok {
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(midTurnSteerMessage(text))})
			a.sink.Emit(event.Event{Kind: event.Steer, Text: text})
		}
		schemas := a.tools.Schemas()
		prefixShape := a.capturePrefixShape(schemas)
		prevPrefixShape := a.lastPrefixShape
		if !a.haveLastPrefixShape {
			prevPrefixShape = prefixShape
		}

		text, reasoning, signature, nativeBlocks, calls, usage, interrupted, partialToolStarted, err := a.stream(ctx, step+1)
		if err != nil {
			a.emitProviderStatus(err)
			if a.onFrontier {
				a.downgradeFromFrontier()
				a.sink.Emit(event.Event{Kind: event.Upgrade, Level: event.LevelWarn, Text: "frontier request failed, switched back to default: " + err.Error()})
				if !interrupted {
					step-- // retry the same round on the default provider
					continue
				}
			}
			if interrupted && streamRecoveries < maxStreamRecoveries {
				streamRecoveries++
				if hasVisibleFinalAnswer(text) {
					a.session.Add(provider.Message{
						Role:               provider.RoleAssistant,
						Content:            text,
						NativeBlocks:       nativeBlocks,
						ReasoningContent:   reasoning,
						ReasoningSignature: signature,
						MemoryCitations:    a.memoryCitations(),
					})
				}
				a.session.Add(provider.Message{
					Role:    provider.RoleUser,
					Content: a.withTurnPreferences(streamRecoveryMessage(hasVisibleFinalAnswer(text), partialToolStarted)),
				})
				a.sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: streamRecoveries, RetryMax: maxStreamRecoveries})
				step-- // recovery retries do not consume the tool-round maxSteps budget
				continue
			}
			return err
		}
		streamRecoveries = 0
		cacheDiagnostics := CompareShape(prevPrefixShape, prefixShape, usage)
		a.lastPrefixShape = prefixShape
		a.haveLastPrefixShape = true
		if usage != nil && usage.TotalTokens > 0 {
			pricing := a.pricing
			profile := a.currentUsageProfile()
			if a.onFrontier {
				pricing = a.frontierPricing
				if _, ok := a.frontierProv.(frontierBudgetTracker); !ok {
					a.frontierTokens.Add(int64(usage.CompletionTokens))
				}
				profile = a.currentUsageProfile()
				if a.emitFrontierBudgetIfExceeded() {
					a.downgradeFromFrontier()
				}
			}
			a.sink.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: pricing,
				Profile:          profile,
				ProviderStatus:   providerStatusForProfile(profile, nil),
				Pxpipe:           clonePxpipeSummary(a.pxpipeSummary),
				CacheDiagnostics: &cacheDiagnostics,
				SessionHit:       int(a.sessCacheHit.Load()), SessionMiss: int(a.sessCacheMiss.Load())})
		}
		if msg, ok := finishReasonMessage(usage); ok {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
		}

		// Keep reasoning_content on the assistant turn for display and session
		// archive. It is NOT re-uploaded to the API: the openai provider drops it
		// when building the request, since re-sent reasoning is billable prompt
		// input for no cache or coherence gain.
		calls = a.withPreviewFileDiffs(calls)
		a.session.Add(provider.Message{
			Role:               provider.RoleAssistant,
			Content:            text,
			NativeBlocks:       nativeBlocks,
			ReasoningContent:   reasoning,
			ReasoningSignature: signature,
			ToolCalls:          calls,
			MemoryCitations:    a.memoryCitations(),
		})

		if len(calls) == 0 {
			readiness := a.finalReadinessCheck()
			if readiness.reason != "" {
				finalReadinessBlocks++
				result := evidence.ReadinessBlocked
				if finalReadinessBlocks >= maxFinalReadinessBlocks {
					result = evidence.ReadinessErrored
					event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
					return fmt.Errorf("final-answer readiness failed %d times: %s", finalReadinessBlocks, readiness.reason)
				}
				event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "final-answer readiness blocked: " + readiness.reason})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(finalReadinessRetryMessage(readiness.reason))})
				a.maybeCompact(ctx, usage)
				continue
			}
			if !hasVisibleFinalAnswer(text) {
				emptyFinalBlocks++
				if emptyFinalBlocks >= maxEmptyFinalBlocks {
					return fmt.Errorf("model finished without a visible final answer %d times", emptyFinalBlocks)
				}
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: emptyFinalNotice(a.prov.Name(), usage, len(reasoning))})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(emptyFinalRetryMessage())})
				a.maybeCompact(ctx, usage)
				continue
			}
			if executorHandoff && !usedAnyTool && handoffNudges < maxExecutorHandoffNudges && shouldNudgeExecutorHandoff(input, text) {
				handoffNudges++
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "executor answered without taking any action; nudging it to use its tools"})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(executorHandoffRetryMessage())})
				a.maybeCompact(ctx, usage)
				continue
			}
			if readiness.applies {
				event.RecordReadinessAudit(a.sink, readiness.audit(evidence.ReadinessAllowed, finalReadinessBlocks > 0))
			}
			if a.steerQueueLen() > 0 {
				continue
			}
			// A final-answer turn otherwise skips compaction, so a large context
			// carries into the next turn un-folded and can overflow the model window.
			// No-op below the trigger, so normal turns keep their warm cache.
			a.maybeCompact(ctx, usage)
			return nil // model gave a final answer
		}
		emptyFinalBlocks = 0
		usedAnyTool = true

		// Grace round guard: if we already gave the model one extra response
		// and it still wants to call tools, stop here.
		if graceRound {
			return fmt.Errorf("paused after %d tool-call rounds (%s) — the work so far is saved; send another message to continue, or set %s higher or to 0 for no limit", a.maxSteps, a.maxStepsKey, a.maxStepsKey)
		}

		results := a.executeBatch(ctx, calls)
		for i, call := range calls {
			a.session.Add(provider.Message{
				Role:       provider.RoleTool,
				Content:    results[i],
				ToolCallID: call.ID,
				Name:       call.Name,
			})
		}
		a.evaluateRoutingAfterTools(ctx, step+1)

		// The prompt only grows from here; compact before the next turn so it
		// stays within the model's window.
		a.maybeCompact(ctx, usage)

		// When the tool-call budget runs out this round, give the model
		// one grace round to produce a final answer from completed work.
		if a.maxSteps > 0 && step+1 >= a.maxSteps {
			graceRound = true
			nudge := fmt.Sprintf("Do not call any more tools — your tool-call round limit (%s) has been reached. Instead, synthesize a final answer from all the work already completed: summarize what was accomplished, what remains to be done, and any decisions the user should make. The user can increase %s or continue in the next turn if more work is needed.", a.maxStepsKey, a.maxStepsKey)
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf("budget (%s=%d) exhausted: one grace round to finalize", a.maxStepsKey, a.maxSteps)})
		}
	}
	// Only reached when a positive maxSteps guard is configured. The work so far
	// is already in the session, so the user can just send another message to pick
	// up where it left off.
	key := strings.TrimSpace(a.maxStepsKey)
	if key == "" {
		key = "agent.max_steps"
	}
	return fmt.Errorf("paused after %d tool-call rounds (%s) — the work so far is saved; send another message to continue, or set max_steps higher or to 0 for no limit", a.maxSteps, key)
}

// resetRoutingForTurn scopes frontier upgrades and the advisor turn budget to a
// single Run: each turn starts on the default provider and re-decides routing
// from this turn's signals (including pending goal control signals applied right
// after). frontierTokens is intentionally not reset — the frontier budget is
// session-wide.
func (a *Agent) resetRoutingForTurn() {
	a.upgraded = false
	a.onFrontier = false
	a.frontierReceiptStart = 0
	a.advisorTurnUses = 0
	if a.defaultProv != nil {
		a.prov = a.defaultProv
		a.pricing = a.defaultPricing
		a.contextWindow = a.defaultContextWindow
	}
}

// RecordControlSignal queues a controller/host observation for the next Run.
// Run resets the per-turn ledger first, then records these signals so routing
// decisions are based on real control-flow events rather than test-only structs.
func (a *Agent) RecordControlSignal(sig evidence.FailureSignal) {
	if a == nil || (sig.GoalAcceptanceLoop <= 0 && !sig.DifficultDecision && strings.TrimSpace(sig.DecisionSummary) == "") {
		return
	}
	a.pendingControlMu.Lock()
	a.pendingControlSignals = append(a.pendingControlSignals, sig)
	a.pendingControlMu.Unlock()
}

func (a *Agent) applyPendingControlSignals() {
	if a == nil || a.evidence == nil {
		return
	}
	a.pendingControlMu.Lock()
	signals := append([]evidence.FailureSignal(nil), a.pendingControlSignals...)
	a.pendingControlSignals = nil
	a.pendingControlMu.Unlock()
	for _, sig := range signals {
		a.evidence.Record(evidence.ControlSignalReceipt(sig))
	}
}

func (a *Agent) evaluateInitialRouting(ctx context.Context) {
	if a.upgradePolicy == nil || a.evidence == nil || a.upgraded || a.frontierProv == nil {
		return
	}
	sig := a.evidence.FailureSignal()
	if sig.GoalAcceptanceLoop <= 0 && !sig.DifficultDecision {
		return
	}
	decision := a.upgradePolicy.Evaluate(sig, 0, a.frontierTokens.Load())
	if decision.TriggerAdvisor {
		a.consultAdvisor(ctx, sig, decision)
	}
	if !decision.ShouldUpgrade {
		return
	}
	a.switchToFrontier(decision)
	target := strings.TrimSpace(decision.TargetModel)
	if target == "" {
		target = strings.TrimSpace(a.frontierTarget)
	}
	if target == "" && a.frontierProv != nil {
		target = a.frontierProv.Name()
	}
	text := "upgraded to " + target
	if strings.TrimSpace(decision.Reason) != "" {
		text += ": " + strings.TrimSpace(decision.Reason)
	}
	a.sink.Emit(event.Event{Kind: event.Upgrade, Level: event.LevelInfo, Text: text})
}

func (a *Agent) evaluateRoutingAfterTools(ctx context.Context, turn int) {
	if a.upgradePolicy == nil || a.evidence == nil {
		return
	}
	if a.onFrontier {
		if a.frontierFailed() {
			a.downgradeFromFrontier()
			a.sink.Emit(event.Event{Kind: event.Upgrade, Level: event.LevelWarn, Text: "frontier also failing, switched back to default"})
		}
		return
	}
	if a.upgraded || a.frontierProv == nil {
		return
	}
	decision := a.upgradePolicy.Evaluate(a.evidence.FailureSignal(), turn, a.frontierTokens.Load())
	if decision.TriggerAdvisor {
		a.consultAdvisor(ctx, a.evidence.FailureSignal(), decision)
	}
	if !decision.ShouldUpgrade {
		return
	}
	a.switchToFrontier(decision)
	target := strings.TrimSpace(decision.TargetModel)
	if target == "" {
		target = strings.TrimSpace(a.frontierTarget)
	}
	if target == "" && a.frontierProv != nil {
		target = a.frontierProv.Name()
	}
	text := "upgraded to " + target
	if strings.TrimSpace(decision.Reason) != "" {
		text += ": " + strings.TrimSpace(decision.Reason)
	}
	a.sink.Emit(event.Event{Kind: event.Upgrade, Level: event.LevelInfo, Text: text})
}

func (a *Agent) consultAdvisor(ctx context.Context, sig evidence.FailureSignal, d UpgradeDecision) {
	if a.advisorRunner == nil {
		return
	}
	turnRemaining, sessionRemaining := a.advisorRemaining()
	if turnRemaining <= 0 || sessionRemaining == 0 {
		return
	}
	req := a.buildAdvisorRequest(sig, d)
	advice, err := a.advisorRunner(ctx, req)
	if err != nil {
		a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelWarn, Text: "advisor consultation failed: " + err.Error(),
			Advisor: event.AdvisorConsultation{Reason: req.Reason, Question: req.Question}})
		return
	}
	advice = strings.TrimSpace(advice)
	if advice == "" {
		return
	}
	a.advisorTurnUses++
	a.advisorSessionUses++
	turnRemaining, sessionRemaining = a.advisorRemaining()
	payload := event.AdvisorConsultation{
		Reason:               req.Reason,
		Question:             req.Question,
		Advice:               advice,
		UsesThisTurn:         a.advisorTurnUses,
		UsesThisSession:      a.advisorSessionUses,
		RemainingThisTurn:    turnRemaining,
		RemainingThisSession: sessionRemaining,
		MaxUsesPerTurn:       a.advisor.MaxUsesPerTurn,
		MaxUsesPerSession:    a.advisor.MaxUsesPerSession,
	}
	a.sink.Emit(event.Event{Kind: event.Advisor, Level: event.LevelInfo, Text: "advisor consulted: " + req.Reason, Advisor: payload})
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: FormatAdvisorGuidance(req, advice, turnRemaining, sessionRemaining)})
}

func (a *Agent) switchToFrontier(d UpgradeDecision) {
	if a.frontierProv == nil {
		return
	}
	if a.defaultProv == nil {
		a.defaultProv = a.prov
		a.defaultPricing = a.pricing
		a.defaultContextWindow = a.contextWindow
	}
	a.prov = a.frontierProv
	a.pricing = a.frontierPricing
	if a.frontierContextWindow > 0 {
		a.contextWindow = a.frontierContextWindow
	}
	if strings.TrimSpace(d.TargetModel) != "" {
		a.frontierTarget = strings.TrimSpace(d.TargetModel)
	}
	a.upgraded = true
	a.onFrontier = true
	a.frontierReceiptStart = a.evidence.Count()
	a.stormSig = ""
	a.stormCount = 0
}

func (a *Agent) downgradeFromFrontier() {
	if a.defaultProv == nil {
		return
	}
	a.prov = a.defaultProv
	a.pricing = a.defaultPricing
	a.contextWindow = a.defaultContextWindow
	a.onFrontier = false
	a.stormSig = ""
	a.stormCount = 0
}

func (a *Agent) frontierFailed() bool {
	if !a.onFrontier || a.evidence == nil {
		return false
	}
	sig := a.evidence.FailureSignalSince(a.frontierReceiptStart)
	if sig.ConsecutiveErrors == 0 {
		return false
	}
	decision := a.upgradePolicy.Evaluate(sig, 0, 0)
	return decision.ShouldUpgrade
}

func (a *Agent) emitFrontierBudgetIfExceeded() bool {
	if tracked, ok := a.frontierProv.(frontierBudgetTracker); ok {
		a.frontierTokens.Store(tracked.OutputTokens())
		if tracked.Exceeded() {
			a.sink.Emit(event.Event{Kind: event.BudgetExceeded, Level: event.LevelWarn,
				Text: fmt.Sprintf("frontier budget exceeded: %d/%d output tokens used", tracked.OutputTokens(), tracked.BudgetLimit())})
			return true
		}
	}
	if limited, ok := a.upgradePolicy.(interface {
		FrontierBudgetLimit() int64
	}); ok {
		limit := limited.FrontierBudgetLimit()
		if limit > 0 && a.frontierTokens.Load() >= limit {
			a.sink.Emit(event.Event{Kind: event.BudgetExceeded, Level: event.LevelWarn,
				Text: fmt.Sprintf("frontier budget exceeded: %d/%d output tokens used", a.frontierTokens.Load(), limit)})
			return true
		}
	}
	return false
}

func (a *Agent) currentUsageProfile() *event.Profile {
	if a == nil || a.prov == nil {
		return nil
	}
	role := "default"
	model := a.prov.Name()
	if a.usageRole != "" {
		role = a.usageRole
	}
	if a.usageModel != "" {
		model = a.usageModel
	}
	if a.onFrontier {
		role = "frontier"
		if target := strings.TrimSpace(a.frontierTarget); target != "" {
			model = target
		}
	}
	profile := &event.Profile{Role: role, Model: strings.TrimSpace(model), Effort: a.usageEffort}
	if used, limit := a.frontierBudgetSnapshot(); limit > 0 {
		profile.BudgetUsed = used
		profile.BudgetLimit = limit
		if used < limit {
			profile.BudgetRemaining = limit - used
		} else {
			profile.BudgetRemaining = 0
		}
	}
	return profile
}

func (a *Agent) emitProviderStatus(err error) {
	status := providerStatusForProfile(a.currentUsageProfile(), err)
	if status == nil {
		return
	}
	a.sink.Emit(event.Event{Kind: event.ProviderStatusUpdate, ProviderStatus: status})
}

func providerStatusForProfile(profile *event.Profile, err error) *event.ProviderStatus {
	if profile == nil {
		return nil
	}
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return nil
	}
	status := &event.ProviderStatus{
		Role:          profile.Role,
		Health:        "ok",
		AuthStatus:    "ok",
		RateLimit:     "ok",
		BalanceStatus: "unknown",
	}
	if err == nil {
		return status
	}
	status.LastError = err.Error()
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		status.Health = "auth_error"
		status.AuthStatus = "auth_error"
		return status
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			status.Health = "auth_error"
			status.AuthStatus = "auth_error"
		case http.StatusTooManyRequests:
			status.Health = "rate_limited"
			status.RateLimit = "rate_limited"
		case http.StatusPaymentRequired:
			status.Health = "balance_error"
			status.BalanceStatus = "insufficient"
		default:
			if apiErr.Status >= 500 && apiErr.Status <= 599 {
				status.Health = "degraded"
			} else {
				status.Health = "error"
			}
		}
		return status
	}
	status.Health = "error"
	return status
}

func (a *Agent) frontierBudgetSnapshot() (used int64, limit int64) {
	if a == nil {
		return 0, 0
	}
	if tracked, ok := a.frontierProv.(frontierBudgetTracker); ok {
		return tracked.OutputTokens(), tracked.BudgetLimit()
	}
	used = a.frontierTokens.Load()
	if limited, ok := a.upgradePolicy.(interface {
		FrontierBudgetLimit() int64
	}); ok {
		limit = limited.FrontierBudgetLimit()
	}
	return used, limit
}

type frontierBudgetTracker interface {
	OutputTokens() int64
	BudgetLimit() int64
	Exceeded() bool
}

func (a *Agent) emitMemoryCompilerStats(turn *memorycompiler.Turn) {
	if a == nil || turn == nil {
		return
	}
	m := turn.Metrics()
	a.sink.Emit(event.Event{Kind: event.MemoryCompilerStatsEvent, MemoryCompiler: &event.MemoryCompilerStats{
		Injected:         m.Injected,
		UsefulIR:         m.UsefulIR,
		CompiledTokens:   m.CompiledTokens,
		IROverheadTokens: m.IROverheadTokens,
		MemoryReferences: m.MemoryReferences,
		Constraints:      m.Constraints,
		RiskNotes:        m.RiskNotes,
		ExecutionSteps:   m.ExecutionSteps,
		TotalNodes:       m.TotalNodes,
		HighSignalNodes:  m.HighSignalNodes,
		ToolResultNodes:  m.ToolResultNodes,
		DecisionNodes:    m.DecisionNodes,
		StrategyCount:    m.StrategyCount,
		LearningCount:    m.LearningCount,
	}})
}

func (a *Agent) finalReadinessFailure() string {
	return a.finalReadinessCheck().reason
}

// GoalReadinessFailure returns the final-readiness failure reason — a summary of
// incomplete todos and unverified project checks — or empty string if none.
// Exported so the Controller can gate [goal:complete] on evidence.
func (a *Agent) GoalReadinessFailure() string {
	return a.finalReadinessFailure()
}

type finalReadinessCheck struct {
	applies              bool
	reason               string
	missingProjectChecks int
	incompleteTodos      int
}

func (c finalReadinessCheck) audit(result evidence.ReadinessAuditResult, recovered bool) evidence.ReadinessAudit {
	return evidence.ReadinessAudit{
		Result:                 result,
		Recovered:              recovered,
		MissingProjectChecks:   c.missingProjectChecks,
		IncompleteTodos:        c.incompleteTodos,
		CommandMismatchMissing: c.missingProjectChecks,
	}
}

func (a *Agent) finalReadinessCheck() finalReadinessCheck {
	if a.evidence == nil {
		return finalReadinessCheck{}
	}
	var missing []string
	out := finalReadinessCheck{}
	if !a.planMode.Load() {
		incomplete, hasTodos := a.evidence.IncompleteLatestTodos()
		if !hasTodos && a.evidence.HasAnySuccessfulReceipt() {
			incomplete, hasTodos = a.incompleteCanonicalTodos()
		}
		if hasTodos && len(incomplete) > 0 && a.evidence.HasSuccessfulTodoProgressReceipt() {
			out.applies = true
			out.incompleteTodos = len(incomplete)
			missing = append(missing, finalReadinessIncompleteTodos(incomplete))
		}
	}
	writer, hasWriter := a.evidence.LatestSuccessfulWriterIndex()
	if !hasWriter {
		if len(missing) > 0 {
			out.reason = strings.Join(missing, "; ")
		}
		return out
	}
	hasProjectChecks := len(a.projectChecks) > 0
	hasTodoReceipt := a.evidence.HasSuccessfulTodoWrite()
	if !hasProjectChecks && !hasTodoReceipt && len(missing) == 0 {
		return finalReadinessCheck{}
	}
	out.applies = true
	for _, check := range a.projectChecks {
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !a.evidence.HasSuccessfulCommandAfter(command, writer) {
			out.missingProjectChecks++
			missing = append(missing, fmt.Sprintf("run %q from %s after the latest write", command, finalReadinessCheckSource(check)))
		}
	}

	if len(missing) == 0 {
		return out
	}
	out.reason = strings.Join(missing, "; ")
	return out
}

func finalReadinessIncompleteTodos(items []evidence.TodoStepMatch) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Content)
		if label == "" {
			label = fmt.Sprintf("todo %d", item.Index)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", label, item.Status))
	}
	return "latest successful todo_write still has incomplete items: " + strings.Join(parts, ", ")
}

func (a *Agent) setTodoState(todos []evidence.TodoItem) {
	a.todoMu.Lock()
	a.todoState = append([]evidence.TodoItem(nil), todos...)
	a.todoMu.Unlock()
}

// SeedTodoState initializes the canonical task list from a host-generated
// starter list, such as an approved plan. A new host seed replaces stale state
// from earlier work so complete_step matches the plan the UI just displayed.
func (a *Agent) SeedTodoState(todos []evidence.TodoItem) {
	if len(todos) == 0 {
		return
	}
	a.setTodoState(todos)
}

// ReplaceTodoState mirrors a host-generated todo list into the canonical state.
// It is used when the host, rather than the model, owns the full state transition.
func (a *Agent) ReplaceTodoState(todos []evidence.TodoItem) {
	a.setTodoState(todos)
	a.recordTodoState(todos)
}

// CanonicalTodoState returns a copy of the host-reconstructed task list.
func (a *Agent) CanonicalTodoState() []evidence.TodoItem {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	return append([]evidence.TodoItem(nil), a.todoState...)
}

func (a *Agent) incompleteCanonicalTodos() ([]evidence.TodoStepMatch, bool) {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	if len(a.todoState) == 0 {
		return nil, false
	}
	return evidence.IncompleteTodos(a.todoState), true
}

// advanceCanonicalTodo flips the canonical todo matching a signed-off step to
// completed (promoting the next pending item to in_progress) and emits a
// synthetic todo_write so the task panel reflects it without the model
// re-sending the whole list. No-op when nothing matches or it is already done.
func (a *Agent) advanceCanonicalTodo(step string) {
	a.todoMu.Lock()
	if len(a.todoState) == 0 {
		a.todoMu.Unlock()
		return
	}
	m, ok := evidence.MatchStep(step, a.todoState)
	if !ok || canonicalTodoStatus(a.todoState[m.Index-1].Status) == "completed" {
		a.todoMu.Unlock()
		return
	}
	a.todoState[m.Index-1].Status = "completed"
	promoteNextPendingTodo(a.todoState)
	snapshot := append([]evidence.TodoItem(nil), a.todoState...)
	a.todoMu.Unlock()
	a.recordTodoState(snapshot)
	a.emitTodoState(snapshot, m.Index)
}

// recordTodoState logs the host-advanced list as a synthetic todo_write receipt
// so the per-turn final gate (which reads the ledger's latest todo_write) sees
// the advance — the model no longer has to re-send a todo_write to mark the
// completion. It bypasses the todo_write tool, so the completion-transition
// guard never runs on it.
func (a *Agent) recordTodoState(todos []evidence.TodoItem) {
	if a.evidence == nil {
		return
	}
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return
	}
	a.evidence.Record(evidence.ReceiptFromToolCall("todo_write", json.RawMessage(args), true, true))
}

func promoteNextPendingTodo(todos []evidence.TodoItem) {
	for _, t := range todos {
		if canonicalTodoStatus(t.Status) == "in_progress" {
			return
		}
	}
	for i := range todos {
		if canonicalTodoStatus(todos[i].Status) == "pending" {
			todos[i].Status = "in_progress"
			return
		}
	}
}

func canonicalTodoStatus(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "pending"
	}
	return s
}

// emitTodoState emits a synthetic todo_write event so the frontend task panel
// reflects a host-advanced completion without the model re-sending the list.
// itemIndex is the 1-based position of the completed todo in the panel.
func (a *Agent) emitTodoState(todos []evidence.TodoItem, itemIndex int) {
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return
	}
	id := fmt.Sprintf("host-advance-%d-%d", a.hostAdvanceSeq.Add(1), itemIndex)
	t := event.Tool{ID: id, Name: "todo_write", Args: string(args), ReadOnly: true}
	a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list advanced by complete_step"
	a.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
}

// RebuildTodoState re-derives canonical task state from the current session
// transcript. Call after externally truncating the session (e.g. after a
// user-cancel strip) so Agent.todoState stays consistent with the messages.
func (a *Agent) RebuildTodoState() {
	a.rebuildTodoState(a.Session().Snapshot())
}

// rebuildTodoState reconstructs the canonical task list from a transcript: the
// latest successful todo_write is the base, then every complete_step after it
// advances an item. Deterministic from persisted messages, so it survives a
// fresh load or a rewind (the truncated history yields the historical state).
// Empty after compaction drops the todo_write — no worse than no canonical list.
func (a *Agent) rebuildTodoState(msgs []provider.Message) {
	successful := successfulToolCallIDs(msgs)
	var todos []evidence.TodoItem
	baseIdx := -1
	for i, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Name != "todo_write" || !successful[tc.ID] {
				continue
			}
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, true)
			// A successful empty todo_write is an explicit clear. Preserve it as the
			// latest base so history reloads do not resurrect an older non-empty list.
			todos = append([]evidence.TodoItem(nil), rec.Todos...)
			baseIdx = i
		}
	}
	if baseIdx < 0 {
		a.setTodoState(nil)
		return
	}
	for i := baseIdx; i < len(msgs); i++ {
		for _, tc := range msgs[i].ToolCalls {
			if tc.Name != "complete_step" || !successful[tc.ID] {
				continue
			}
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, true)
			if m, ok := evidence.MatchStep(rec.Step, todos); ok && canonicalTodoStatus(todos[m.Index-1].Status) != "completed" {
				todos[m.Index-1].Status = "completed"
				promoteNextPendingTodo(todos)
			}
		}
	}
	a.setTodoState(todos)
}

func successfulToolCallIDs(msgs []provider.Message) map[string]bool {
	successful := map[string]bool{}
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool || msg.ToolCallID == "" {
			continue
		}
		if !toolResultFailed(msg.Content) {
			successful[msg.ToolCallID] = true
		}
	}
	return successful
}

func toolResultFailed(content string) bool {
	content = strings.TrimSpace(content)
	return strings.HasPrefix(content, "error:") ||
		strings.HasPrefix(content, "blocked:") ||
		strings.HasPrefix(content, "Error:") ||
		strings.HasPrefix(content, "[error")
}

func finalReadinessCheckSource(check instruction.VerifyCheck) string {
	source := strings.TrimSpace(check.SourcePath)
	if source == "" {
		source = "project memory"
	}
	if check.Line > 0 {
		return fmt.Sprintf("%s:%d", source, check.Line)
	}
	return source
}

func finalReadinessRetryMessage(reason string) string {
	return "Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: " + reason + ". Run the required tool calls, then answer when readiness is satisfied. If the blocked item needs user input, a user-owned choice, or manual review, call the ask tool with concrete options and wait for its tool result; do not ask in prose, and do not claim the user answered unless an actual ask tool result or a new user message says so."
}

func shouldNudgeExecutorHandoff(input, answer string) bool {
	return !executorHandoffAllowsTextOnly(input, answer)
}

func executorHandoffAllowsTextOnly(input, answer string) bool {
	if looksLikeExecutorHandoffDeferral(answer) {
		return false
	}
	task, plan, ok := parseExecutorHandoff(input)
	if !ok {
		return false
	}
	if handoffTaskLooksTextOnly(task) {
		return true
	}
	return handoffPlanLooksTextOnly(plan)
}

func parseExecutorHandoff(input string) (task, plan string, ok bool) {
	input = StripTransientUserBlocks(input)
	marker := "# " + executorHandoffMarker
	i := strings.Index(input, marker)
	if i < 0 {
		return "", "", false
	}
	input = input[i+len(marker):]
	_, input, ok = strings.Cut(input, "\n\nOriginal task:\n")
	if !ok {
		return "", "", false
	}
	task, input, ok = strings.Cut(input, "\n\nPlanner output:\n")
	if !ok {
		return "", "", false
	}
	plan, _, ok = strings.Cut(input, "\n\nExecutor instructions:")
	if !ok {
		return "", "", false
	}
	if beforeToolContext, _, found := strings.Cut(plan, "\n\nExecutor tool context:"); found {
		plan = beforeToolContext
	}
	return strings.TrimSpace(task), strings.TrimSpace(plan), true
}

func looksLikeExecutorHandoffDeferral(answer string) bool {
	lower := strings.ToLower(strings.TrimSpace(answer))
	if lower == "" {
		return true
	}
	if containsAnySubstring(lower, executorHandoffDeferralPhrases) {
		return true
	}
	switch strings.Trim(lower, " \t\r\n.!?。！？") {
	case "ok", "okay", "sounds good", "done", "好的", "可以", "没问题", "收到":
		return true
	default:
		return false
	}
}

func handoffTaskLooksTextOnly(task string) bool {
	lower := strings.ToLower(strings.TrimSpace(task))
	if lower == "" {
		return false
	}
	if containsAnySubstring(lower, executorHandoffWorkRequestTerms) {
		return false
	}
	return containsAnySubstring(lower, executorHandoffTextOnlyTaskTerms)
}

func handoffPlanLooksTextOnly(plan string) bool {
	lower := strings.ToLower(strings.TrimSpace(plan))
	if lower == "" {
		return false
	}
	if containsAnySubstring(lower, executorHandoffLocalActionTerms) {
		return false
	}
	if containsAnySubstring(lower, executorHandoffTextOnlyPlanTerms) {
		return true
	}
	return strings.Contains(lower, "?")
}

func containsAnySubstring(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

var executorHandoffDeferralPhrases = []string{
	"plan looks", "looks good", "should be easy", "should be straightforward",
	"i can implement", "i'll implement", "i will implement", "i'll get started",
	"let me ", "i will now", "i'll now", "i can do that",
	"计划看起来", "可以实现", "我会", "我将", "接下来我", "马上开始",
}

var executorHandoffWorkRequestTerms = []string{
	"implement", "fix", "refactor", "migrate", "edit", "write", "create", "delete",
	"update", "remove", "add ", "test", "build", "repair", "patch",
	"修改", "修复", "实现", "新增", "重构", "迁移", "补齐", "更新", "删除", "移除",
}

var executorHandoffTextOnlyTaskTerms = []string{
	"now what", "what next", "tl;dr", "tldr", "summarize", "summary", "explain",
	"i installed", "i just installed", "i turned on", "i enabled", "it's on", "it is on",
	"怎么办", "下一步", "然后呢", "总结", "解释", "说明", "装了", "装好了", "安装了", "开了", "开启了", "打开了",
}

var executorHandoffLocalActionTerms = []string{
	"write_file", "read_file", "apply_patch", "bash",
	"workspace", "repo", "repository", "codebase", "file", "path",
	"write ", "edit ", "modify ", "create ", "delete ", "remove ", "update ", "add ", "patch ", "refactor ", "implement ",
	"run ", "command", "test", "build",
	"文件", "路径", "仓库", "代码", "写入", "编辑", "修改", "创建", "删除", "移除", "更新", "新增", "运行", "命令", "测试", "构建",
}

var executorHandoffTextOnlyPlanTerms = []string{
	"tell the user", "ask the user", "guide the user", "explain to the user",
	"summarize", "summary", "tl;dr", "tldr", "answer the user", "respond to the user",
	"provide guidance", "walk the user", "instruct the user", "have the user",
	"user should", "the user should", "user can", "the user can", "manual", "manually",
	"no tools needed", "no tool calls needed", "does not need tools", "needs no tools",
	"listen", "play a song", "compare the difference", "checkbox",
	"告诉用户", "询问用户", "问用户", "让用户", "请用户", "指导用户", "解释", "总结", "回答",
	"手动", "无需工具", "不需要工具", "试听", "听歌", "对比", "勾选",
}

func executorHandoffRetryMessage() string {
	return `You are already in the executor phase. The planner's read-only limitations do not apply to you.

The tool schema is still attached to this executor request. Do not invent that MCP servers or tools are unavailable; only report an unavailable tool after a real tool call or host error proves it.

Do not answer as the planner and do not ask how to trigger the executor.
Use your available tools now to carry out the task. If carrying out the planner's instructions requires a user-owned choice or review, call the ask tool with concrete options and wait for its tool result; do not ask in prose, and do not claim the user answered unless an actual ask tool result or a new user message says so. If a write or command is blocked by permissions or workspace boundaries, state that specific blocker and ask for the needed approval/path.`
}

func hasVisibleFinalAnswer(text string) bool {
	return strings.TrimSpace(text) != ""
}

func emptyFinalRetryMessage() string {
	return "The previous assistant response finished without any visible answer text. Continue the same task now and provide a concise visible answer to the user. Do not send reasoning only."
}

func emptyFinalNotice(prov string, u *provider.Usage, reasoningLen int) string {
	finish := "unknown"
	if u != nil && u.FinishReason != "" {
		finish = u.FinishReason
	}
	return fmt.Sprintf("empty final answer blocked: %s returned no visible answer text (finish=%s, reasoning=%d chars); retrying", prov, finish, reasoningLen)
}

func streamRecoveryMessage(hasPartialText, hadPartialTool bool) string {
	switch {
	case hadPartialTool:
		return "The previous assistant response was interrupted while a tool call was streaming. Continue the same task now. If a tool is still needed, issue a fresh complete tool call from scratch; do not rely on any partial tool-call arguments from the interrupted stream."
	case hasPartialText:
		return "The previous assistant response was interrupted during streaming. Continue the same task from immediately after the partial assistant message above. Do not repeat text that is already visible."
	default:
		return "The previous assistant response was interrupted during streaming before visible answer text was completed. Continue the same task now and provide the next useful response."
	}
}

// stream runs one completion, emitting reasoning and text deltas as typed
// events and collecting complete tool calls. A Message event closes the text
// stream so a sink can re-render the streamed raw text as styled markdown. The
// accumulated text and reasoning are also returned so the caller can round-trip
// reasoning on the next turn.
func (a *Agent) stream(ctx context.Context, turn int) (string, string, string, []json.RawMessage, []provider.ToolCall, *provider.Usage, bool, bool, error) {
	ctx = provider.WithRetryNotify(ctx, func(info provider.RetryInfo) {
		a.sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: info.Attempt, RetryMax: info.Max})
	})
	ch, err := a.prov.Stream(ctx, provider.Request{
		Messages:      a.session.Messages,
		Tools:         a.tools.Schemas(),
		Temperature:   a.temperature,
		NativeAdvisor: a.nativeAdvisorForRequest(),
	})
	if err != nil {
		return "", "", "", nil, nil, nil, false, false, err
	}

	// A PostLLMCall hook rewrites the whole reasoning block, so when one is wired
	// up we buffer reasoning silently and emit the transformed text once after the
	// stream. With no such hook the reasoning streams live, chunk by chunk, as
	// before — the common case must not lose its live "thinking…" display.
	transformReasoning := a.hooks != nil && a.hooks.HasPostLLMCall()

	var text, reasoning strings.Builder
	var signature string // provider-issued proof for the reasoning (Anthropic thinking)
	var nativeBlocks []json.RawMessage
	var calls []provider.ToolCall
	var usage *provider.Usage
	var partialToolStarted bool
	finishReasoning := func() (stored, display string) {
		original := reasoning.String()
		display = original
		if transformReasoning && original != "" {
			display = a.hooks.PostLLMCall(ctx, original, turn)
			if display != "" {
				a.sink.Emit(event.Event{Kind: event.Reasoning, Text: display})
			}
		}
		stored = display
		if signature != "" {
			stored = original
		}
		return stored, display
	}
	for {
		var chunk provider.Chunk
		select {
		case <-ctx.Done():
			stored, _ := finishReasoning()
			return text.String(), stored, signature, nativeBlocks, calls, usage, false, partialToolStarted, ctx.Err()
		case c, ok := <-ch:
			if !ok {
				if err := ctx.Err(); err != nil {
					stored, _ := finishReasoning()
					return text.String(), stored, signature, nativeBlocks, calls, usage, false, partialToolStarted, err
				}
				stored, display := finishReasoning()
				if text.Len() > 0 || display != "" {
					a.sink.Emit(event.Event{
						Kind:            event.Message,
						Text:            StripGoalMarkers(text.String()),
						Reasoning:       display,
						MemoryCitations: a.memoryCitations(),
					})
				}
				return text.String(), stored, signature, nativeBlocks, calls, usage, false, false, nil
			}
			chunk = c
		}
		switch chunk.Type {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
			if chunk.Signature != "" {
				signature = chunk.Signature
			}
			if chunk.Text != "" && !transformReasoning {
				a.sink.Emit(event.Event{Kind: event.Reasoning, Text: chunk.Text})
			}
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			a.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text})
		case provider.ChunkToolCallStart:
			partialToolStarted = true
			// Surface the tool card as soon as the call begins — before its
			// (possibly large) arguments finish streaming — so the user sees it
			// working instead of a stall. executeBatch emits the full dispatch
			// (with args) once the call completes; the frontend merges by ID.
			if tc := chunk.ToolCall; tc != nil {
				a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
					ID: tc.ID, Name: tc.Name, ReadOnly: a.toolReadOnly(tc.Name), Partial: true,
				}})
			}
		case provider.ChunkToolCall:
			partialToolStarted = true
			calls = append(calls, *chunk.ToolCall)
		case provider.ChunkUsage:
			usage = chunk.Usage
			a.lastUsage.Store(chunk.Usage)
			a.sessCacheHit.Add(int64(chunk.Usage.CacheHitTokens))
			a.sessCacheMiss.Add(int64(chunk.Usage.CacheMissTokens))
		case provider.ChunkError:
			if provider.IsStreamInterrupted(chunk.Err) {
				stored, _ := finishReasoning()
				return text.String(), stored, signature, nativeBlocks, calls, usage, true, partialToolStarted, chunk.Err
			}
			return "", "", "", nil, nil, nil, false, false, chunk.Err
		case provider.ChunkNativeBlock:
			if len(chunk.NativeBlock) > 0 {
				block := make(json.RawMessage, len(chunk.NativeBlock))
				copy(block, chunk.NativeBlock)
				nativeBlocks = append(nativeBlocks, block)
			}
		}
	}
}

func (a *Agent) memoryCitations() []provider.MemoryCitation {
	if a.compilerTurn == nil {
		return nil
	}
	return a.compilerTurn.MemoryCitations()
}

func (a *Agent) capturePrefixShape(schemas []provider.ToolSchema) PrefixShape {
	return CaptureShape(a.systemPrompt(), schemas, a.session.RewriteVersion())
}

func (a *Agent) systemPrompt() string {
	var b strings.Builder
	for _, m := range a.session.Messages {
		if m.Role != provider.RoleSystem {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// executeBatch dispatches one model turn's tool calls. A ToolDispatch event is
// emitted for every call up front, in call order, so a frontend can show the
// timeline chronologically. Contiguous known ReadOnly calls fan out across
// goroutines; unknown and writer calls run as single-call serial segments so
// write/read ordering stays provider-ordered. ToolResult events are emitted
// after the batch in call order, so emission stays serial even when execution
// parallelised.
func (a *Agent) executeBatch(ctx context.Context, calls []provider.ToolCall) []string {
	for _, c := range calls {
		t, ok := a.tools.Get(c.Name)
		ev := event.Tool{ID: c.ID, Name: c.Name, Args: c.Arguments, ReadOnly: ok && t.ReadOnly()}
		ev.FileDiff = event.FileDiff{Diff: c.Diff, Added: c.Added, Removed: c.Removed}
		if ok && ev.Diff == "" && ev.Added == 0 && ev.Removed == 0 {
			if ch, ok := tool.PreviewChange(t, json.RawMessage(c.Arguments)); ok {
				ev.FileDiff = event.FileDiff{Diff: ch.Diff, Added: ch.Added, Removed: ch.Removed}
			}
		}
		if ok {
			if pr, ok := t.(interface {
				ResolveProfile(json.RawMessage) *event.Profile
			}); ok {
				ev.Profile = pr.ResolveProfile(json.RawMessage(c.Arguments))
			}
		}
		a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: ev})
	}

	results := make([]string, len(calls))
	outcomes := make([]toolOutcome, len(calls))
	durations := make([]int64, len(calls))
	run := func(i int) {
		start := time.Now()
		outcomes[i] = a.executeOne(ctx, calls[i])
		durations[i] = time.Since(start).Milliseconds()
		results[i] = outcomes[i].output
	}
	cancelled := false
	markCancelled := func(start int) {
		errMsg := context.Canceled.Error()
		if err := ctx.Err(); err != nil {
			errMsg = err.Error()
		}
		output := "cancelled: context cancelled before execution"
		for j := start; j < len(calls); j++ {
			results[j] = output
			outcomes[j] = toolOutcome{output: output, errMsg: errMsg}
		}
		cancelled = true
	}

	for _, batch := range partitionToolCalls(a.tools, calls) {
		if ctx.Err() != nil {
			markCancelled(batch.start)
			break
		}
		if batch.parallel && batch.end-batch.start > 1 {
			ranUntil := runParallel(ctx, a.maxParallelTools, batch.start, batch.end, run)
			// After parallel execution completes, check if context was cancelled.
			// The individual tool executions should have detected ctx.Done(), but
			// we verify here to ensure we don't continue to subsequent batches.
			if ctx.Err() != nil {
				markCancelled(ranUntil)
				break
			}
			continue
		}
		for i := batch.start; i < batch.end; i++ {
			// Before executing the next tool, check if context was cancelled.
			// This prevents starting new tools when a previous tool's execution
			// triggered cancellation.
			if ctx.Err() != nil {
				markCancelled(i)
				break
			}
			run(i)
			// After each tool execution, also check if the context was cancelled.
			// If so, stop executing remaining tools and return immediately so
			// the agent loop can detect the cancellation and exit.
			if ctx.Err() != nil {
				markCancelled(i + 1)
				break
			}
		}
		if cancelled {
			break
		}
	}

	for i, c := range calls {
		o := outcomes[i]
		t, ok := a.tools.Get(c.Name)
		a.recordToolCompression(c.Name, o.compression)
		a.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
			ID:          c.ID,
			Name:        c.Name,
			Args:        c.Arguments,
			Output:      o.output,
			Err:         o.errMsg,
			ReadOnly:    ok && t.ReadOnly(),
			Truncated:   o.truncated,
			DurationMs:  durations[i],
			Compression: o.compression,
		}})
		if o.truncated && o.truncMsg != "" {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: o.truncMsg})
		}
	}
	if a.compilerTurn != nil {
		records := make([]memorycompiler.ToolRecord, 0, len(calls))
		for i, c := range calls {
			o := outcomes[i]
			t, ok := a.tools.Get(c.Name)
			records = append(records, memorycompiler.ToolRecord{
				ID:         c.ID,
				Name:       c.Name,
				Args:       c.Arguments,
				Output:     o.output,
				Error:      o.errMsg,
				ReadOnly:   ok && t.ReadOnly(),
				DurationMs: durations[i],
				Truncated:  o.truncated,
			})
		}
		a.compilerTurn.RecordToolResults(records)
	}
	if !cancelled {
		a.applyStormBreaker(calls, outcomes, results)
	}
	return results
}

func (a *Agent) withPreviewFileDiffs(calls []provider.ToolCall) []provider.ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]provider.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if out[i].Diff != "" || out[i].Added != 0 || out[i].Removed != 0 {
			continue
		}
		t, ok := a.tools.Get(out[i].Name)
		if !ok {
			continue
		}
		if ch, ok := tool.PreviewChange(t, json.RawMessage(out[i].Arguments)); ok {
			out[i].Diff = ch.Diff
			out[i].Added = ch.Added
			out[i].Removed = ch.Removed
		}
	}
	return out
}

type toolCallBatch struct {
	start    int
	end      int
	parallel bool
}

// partitionToolCalls keeps provider order while letting contiguous known
// read-only tools run together. Unknown and writer tools are single-call serial
// batches so they cannot reorder around reads or produce surprising errors.
// complete_step and todo_write are read-only but never join a parallel run: they
// read the turn's evidence ledger, so every prior call's receipt must be recorded
// before they run.
func partitionToolCalls(r *tool.Registry, calls []provider.ToolCall) []toolCallBatch {
	var batches []toolCallBatch
	for i := 0; i < len(calls); {
		if parallelisable(r, calls[i].Name) {
			start := i
			i++
			for i < len(calls) && parallelisable(r, calls[i].Name) {
				i++
			}
			batches = append(batches, toolCallBatch{start: start, end: i, parallel: true})
			continue
		}
		batches = append(batches, toolCallBatch{start: i, end: i + 1})
		i++
	}
	return batches
}

func parallelisable(r *tool.Registry, name string) bool {
	if name == "complete_step" || name == "todo_write" {
		return false
	}
	t, ok := r.Get(name)
	return ok && t.ReadOnly()
}

// defaultMaxParallelTools is the concurrency cap applied when the
// agent.max_parallel_tools config is unset (or <= 0).
const defaultMaxParallelTools = 8

func runParallel(ctx context.Context, limit, start, end int, run func(int)) int {
	if limit <= 0 {
		limit = defaultMaxParallelTools
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	ranUntil := start
launch:
	for i := start; i < end; i++ {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		i := i
		wg.Add(1)
		ranUntil = i + 1
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			run(i)
		}()
	}
	wg.Wait()
	return ranUntil
}

// stormBreakThreshold is how many times in a row the same tool may fail the same
// way before the loop stops echoing the raw error back and instead returns a
// directive to change approach. Two natural self-corrections are healthy; the
// third identical failure is a death-spiral — the dominant case being a tool call
// whose arguments are truncated at the output-token ceiling, which the model then
// re-emits (re-worded but still over-long), truncating the same way again.
const stormBreakThreshold = 3

// repeatSuccessBreakThreshold is how many identical write-like successes the
// agent allows before refusing another copy in the same user turn. Two gives the
// model room for a natural self-correction; the third repeat is usually a
// no-op/write loop and should be redirected to a different tool or final answer.
const repeatSuccessBreakThreshold = 2

// applyStormBreaker detects a run of identically-failing turns and, past the
// threshold, rewrites the model-facing result (results[0]) into a directive to
// change approach. It keys on each call's (tool, error) — not its args — because a
// stuck model reworks the arguments cosmetically while failing identically (see
// the stormSig field doc). A turn is a fixation candidate only when every one of
// its calls errored and none was merely blocked by plan mode / permissions (those
// carry a clear, distinct message the model can already act on). Any success, any
// block, or a different batch shape is varied work, so it resets the counter. This
// covers both the single-call spiral and a repeated multi-call batch. The hard
// maxSteps guard remains the ultimate backstop; this just keeps the loop from
// burning that whole budget bouncing off the same failure.
func (a *Agent) applyStormBreaker(calls []provider.ToolCall, outcomes []toolOutcome, results []string) {
	sig, ok := batchStormSignature(calls, outcomes)
	if !ok {
		a.stormSig, a.stormCount = "", 0
		return
	}
	if sig != a.stormSig {
		a.stormSig, a.stormCount = sig, 1
		return
	}
	a.stormCount++
	if a.stormCount < stormBreakThreshold {
		return
	}
	subject := fmt.Sprintf("%q", calls[0].Name)
	short := calls[0].Name
	if len(calls) > 1 {
		subject = fmt.Sprintf("this batch of %d tool calls", len(calls))
		short = fmt.Sprintf("a batch of %d calls", len(calls))
	}
	results[0] = outcomes[0].output + fmt.Sprintf(
		"\n\n[loop guard] %s has now failed %d times in a row with the same error. Re-sending it — even with the wording changed — will not help: the calls keep failing the same way. Change approach: if an argument is being truncated, write less in one call and split the work into several smaller calls; otherwise fix the arguments, use a different tool, or explain the blocker in your final answer.",
		subject, a.stormCount)
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: fmt.Sprintf(
		"loop guard: %s failed %d× the same way — nudging the model to change approach",
		short, a.stormCount)})
}

// batchStormSignature returns a per-turn fixation signature — each call's
// (name, error) in order — and ok=true only when every call errored and none was
// merely blocked. ok=false (any success or block) means the turn made varied
// progress, so the caller resets the counter. Keying on the error rather than the
// args is deliberate: a stuck model reworks the arguments while failing the same
// way, so identical-args matching would miss the loop.
func batchStormSignature(calls []provider.ToolCall, outcomes []toolOutcome) (string, bool) {
	if len(calls) == 0 {
		return "", false
	}
	var sb strings.Builder
	for i := range calls {
		if outcomes[i].errMsg == "" || outcomes[i].blocked {
			return "", false
		}
		sb.WriteString(calls[i].Name)
		sb.WriteByte(0)
		sb.WriteString(outcomes[i].errMsg)
		sb.WriteByte(0)
	}
	return sb.String(), true
}

// toolOutcome is one tool call's result, split into the model-facing output and
// the display-facing notice bits. errMsg is the short failure reason (empty on
// success) — a refused call, an unknown tool, or an execution error — so a sink
// renders the result as failed ("⊘ name <errMsg>" / a red card) instead of OK;
// blocked narrows that to a refusal (plan mode / permission). truncMsg is set
// (without the "· " prefix) when the output was head+tailed.
type toolOutcome struct {
	output      string
	blocked     bool
	errMsg      string
	truncated   bool
	truncMsg    string
	compression *event.Compression
}

// executeOne runs a single tool call. It is pure with respect to the event sink
// — the caller emits ToolDispatch/ToolResult — so it is safe to invoke from
// parallel goroutines.
func (a *Agent) executeOne(ctx context.Context, call provider.ToolCall) toolOutcome {
	t, ok := a.tools.Get(call.Name)
	if !ok {
		return toolOutcome{
			output: fmt.Sprintf("error: unknown tool %q", call.Name),
			errMsg: fmt.Sprintf("unknown tool %q", call.Name),
		}
	}
	if out, blocked := a.repeatedSuccessBlock(call, t); blocked {
		return toolOutcome{
			output:  out,
			blocked: true,
			errMsg:  "blocked by loop guard",
		}
	}
	if a.planMode.Load() {
		// Translate the tool's optional plan-mode self-report into the policy's
		// tri-state. Mirrors the t.(tool.Previewer) assertion precedent below.
		safety := planmode.PlanSafetyUnknown
		if c, ok := t.(tool.PlanModeClassifier); ok {
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		// External tools (MCP) whose ReadOnly() is only a server-reported
		// readOnlyHint are not trusted by plan mode's read-only fast path.
		untrusted := false
		if u, ok := t.(tool.PlanModeUntrustedReadOnly); ok {
			untrusted = u.PlanModeUntrustedReadOnly()
		}
		if blocked, msg := a.planModeBlocked(call.Name, t.ReadOnly(), untrusted, safety, json.RawMessage(call.Arguments)); blocked {
			trustAllowed := false
			if t.ReadOnly() && untrusted && safety != planmode.PlanSafetyUnsafe {
				if allow, outcome, handled := a.checkPlanModeReadOnlyTrust(ctx, call, t); handled {
					if !allow {
						return outcome
					}
					trustAllowed = true
				}
			}
			if !trustAllowed {
				return toolOutcome{
					output:  msg,
					blocked: true,
					errMsg:  "blocked: plan mode is read-only",
				}
			}
		}
	}
	if a.gate != nil {
		allow, reason, err := a.gate.Check(ctx, call.Name, json.RawMessage(call.Arguments), t.ReadOnly())
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", err),
			}
		}
		if !allow {
			return toolOutcome{
				output:  "blocked: " + reason,
				blocked: true,
				errMsg:  "blocked by permission policy",
			}
		}
	}
	// PreToolUse hooks run after permission is granted but before the call: a
	// gating hook (exit 2) refuses it, surfaced to the model like a gate denial.
	if a.hooks != nil {
		if block, msg := a.hooks.PreToolUse(ctx, call.Name, json.RawMessage(call.Arguments)); block {
			if msg == "" {
				msg = "blocked by a PreToolUse hook"
			}
			return toolOutcome{
				output:  "blocked: " + msg,
				blocked: true,
				errMsg:  "blocked by PreToolUse hook",
			}
		}
	}
	// Checkpoint the file this writer is about to change, so the turn can be
	// rewound. Fires after all gating (the edit is cleared to run) and only for
	// tools that can describe their change; a Preview error means the edit will
	// likely fail anyway, so we skip rather than snapshot a stale state.
	if a.onPreEdit != nil && !t.ReadOnly() {
		if pv, ok := t.(tool.Previewer); ok {
			if change, perr := pv.Preview(json.RawMessage(call.Arguments)); perr == nil {
				a.onPreEdit(change)
			}
		}
	}
	cctx := withCallContext(ctx, call.ID, a.sink, a.asker, a.planMode.Load())
	if a.evidence != nil {
		cctx = evidence.WithLedger(cctx, a.evidence)
		cctx = evidence.WithSessionMessages(cctx, a.session.Snapshot())
	}
	if len(a.projectChecks) > 0 {
		cctx = instruction.WithChecks(cctx, a.projectChecks)
	}
	if a.jobs != nil {
		cctx = jobs.WithManager(cctx, a.jobs)
	}
	if v := a.responseLanguage.Load(); v != nil {
		if lang, ok := v.(string); ok {
			cctx = WithResponseLanguagePreference(cctx, lang)
		}
	}
	if v := a.reasoningLanguage.Load(); v != nil {
		if lang, ok := v.(string); ok {
			cctx = WithReasoningLanguagePreference(cctx, lang)
		}
	}
	if a.memQueue != nil {
		cctx = memory.WithQueue(cctx, a.memQueue)
	}
	callID := call.ID
	cctx = tool.WithProgress(cctx, func(chunk string) {
		a.sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: callID, Output: chunk}})
	})
	result, err := t.Execute(cctx, json.RawMessage(call.Arguments))
	if a.evidence != nil {
		if call.Name == "complete_step" {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			a.evidence.Record(rec)
			if err == nil {
				a.advanceCanonicalTodo(rec.Step)
			}
		} else {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			a.evidence.Record(rec)
			if err == nil && call.Name == "todo_write" {
				a.setTodoState(rec.Todos)
			}
		}
	}
	// PostToolUse hooks observe the result (they can't block); fired whether the
	// call succeeded or errored, since the tool did run.
	if a.hooks != nil {
		a.hooks.PostToolUse(ctx, call.Name, json.RawMessage(call.Arguments), result)
	}
	if err != nil {
		detail := result
		// Malformed-args failures are a transient model JSON glitch (e.g. options
		// written as ["a":"b"] → "invalid character ':' after array element"). The
		// args can't be safely re-parsed, but echoing the tool's schema makes the
		// retry land valid instead of repeating the same broken shape.
		if !json.Valid([]byte(call.Arguments)) {
			detail = strings.TrimRight(detail, "\n") + "\nThe arguments were not valid JSON. Re-emit them exactly per this schema:\n" + string(t.Schema())
		}
		body, truncMsg, compression := a.modelToolOutput(call, t, fmt.Sprintf("error: %v\n%s", err, detail))
		return toolOutcome{output: body, errMsg: firstLine(err.Error()), truncated: truncMsg != "", truncMsg: truncMsg, compression: compression}
	}
	a.recordRepeatSuccess(call, t)
	// A foreground `task` sub-agent just finished — its result is the final answer.
	// (A backgrounded one returns a "Started…" string and stops later in a job, so
	// it doesn't fire here.) SubagentStop lets a hook react to delegated work.
	if a.hooks != nil && call.Name == "task" && !isBackgroundTaskCall(call.Arguments) {
		a.hooks.SubagentStop(ctx, result)
	}
	body, truncMsg, compression := a.modelToolOutput(call, t, result)
	return toolOutcome{output: body, truncated: truncMsg != "", truncMsg: truncMsg, compression: compression}
}

func (a *Agent) modelToolOutput(call provider.ToolCall, t tool.Tool, raw string) (string, string, *event.Compression) {
	body := raw
	var compression *event.Compression
	if call.Name == "tool_result" {
		output, truncMsg := truncateToolOutput(body)
		return output, truncMsg, nil
	}
	if compressed, meta, ok := a.compressToolOutput(call, t, raw); ok {
		body = compressed
		compression = meta
	} else if a.toolOutputCompressor != nil {
		a.clearRawToolResult(call.ID)
	}
	output, truncMsg := truncateToolOutput(body)
	if compression != nil {
		updateToolCompressionVisibleMetrics(compression, output)
	}
	return output, truncMsg, compression
}

func (a *Agent) compressToolOutput(call provider.ToolCall, t tool.Tool, raw string) (string, *event.Compression, bool) {
	if a == nil || a.toolOutputCompressor == nil || call.ID == "" {
		return "", nil, false
	}
	rawRef := "raw://tool/" + call.ID
	opts := a.toolOutputCompression
	opts.RawRef = rawRef
	result, recovered := func() (result contextpack.Result, recovered any) {
		defer func() {
			if r := recover(); r != nil {
				recovered = r
			}
		}()
		result = a.toolOutputCompressor.Compress(contextpack.ToolOutput{
			ToolName: call.Name,
			Args:     call.Arguments,
			Output:   raw,
			ReadOnly: t.ReadOnly(),
		}, opts)
		return result, nil
	}()
	if recovered != nil {
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: fmt.Sprintf(
			"tool output compression failed for %s: %v; using raw/truncated output",
			call.Name, recovered)})
		return "", nil, false
	}
	if !result.Compressed || result.Content == "" {
		return "", nil, false
	}
	compressed := formatCompressedToolOutput(result, rawRef)
	if compressed == "" || len(compressed) >= len(raw) {
		return "", nil, false
	}
	if err := a.storeRawToolResult(call.ID, raw); err != nil {
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: fmt.Sprintf(
			"raw tool result store failed for %s (%s): %v; full output unavailable",
			call.Name, call.ID, err)})
		compressedOnly := formatCompressedToolOutput(result, "")
		if compressedOnly == "" {
			compressedOnly = result.Content
		}
		return compressedOnly, toolCompressionEvent(result, ""), true
	}
	return compressed, toolCompressionEvent(result, rawRef), true
}

func toolCompressionEvent(result contextpack.Result, rawRef string) *event.Compression {
	return &event.Compression{
		RawRef:           rawRef,
		Strategy:         result.Strategy,
		Summary:          result.Summary,
		RawChars:         result.RawChars,
		CompressedChars:  result.CompressedChars,
		SavedChars:       result.SavedChars,
		RawTokens:        result.RawTokens,
		CompressedTokens: result.CompressedTokens,
		SavedTokens:      result.SavedTokens,
	}
}

func updateToolCompressionVisibleMetrics(c *event.Compression, visible string) {
	c.CompressedChars = utf8.RuneCountInString(visible)
	if c.RawChars > c.CompressedChars {
		c.SavedChars = c.RawChars - c.CompressedChars
	} else {
		c.SavedChars = 0
	}
	c.CompressedTokens = estimateToolCompressionTokens(visible)
	if c.RawTokens > c.CompressedTokens {
		c.SavedTokens = c.RawTokens - c.CompressedTokens
	} else {
		c.SavedTokens = 0
	}
}

func estimateToolCompressionTokens(s string) int {
	if s == "" {
		return 0
	}
	ascii := 0
	cjk := 0
	other := 0
	for _, r := range s {
		switch {
		case r <= 0x7f:
			ascii++
		case (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf) || (r >= 0x3040 && r <= 0x30ff) || (r >= 0xac00 && r <= 0xd7af):
			cjk++
		default:
			other++
		}
	}
	tokens := (ascii + 3) / 4
	tokens += cjk
	tokens += (other + 1) / 2
	if tokens == 0 {
		return 1
	}
	return tokens
}

func formatCompressedToolOutput(result contextpack.Result, rawRef string) string {
	content := strings.TrimRight(result.Content, "\n")
	var note strings.Builder
	note.WriteString("[compressed tool output")
	if result.Summary != "" {
		note.WriteString(": ")
		note.WriteString(result.Summary)
	}
	if rawRef != "" {
		note.WriteString("; raw available at ")
		note.WriteString(rawRef)
	}
	note.WriteByte(']')
	if content == "" {
		return note.String()
	}
	return content + "\n\n" + note.String()
}

func (a *Agent) checkPlanModeReadOnlyTrust(ctx context.Context, call provider.ToolCall, t tool.Tool) (bool, toolOutcome, bool) {
	if a.planModeReadOnlyTrust == nil {
		return false, toolOutcome{}, false
	}
	server, rawTool, ok := planModeMCPTrustTarget(call.Name, t)
	if !ok {
		return false, toolOutcome{}, false
	}
	req := PlanModeReadOnlyTrustRequest{
		ToolName:    call.Name,
		ServerName:  server,
		RawToolName: rawTool,
		Args:        json.RawMessage(call.Arguments),
	}
	allow, reason, err := a.planModeReadOnlyTrust.CheckPlanModeReadOnlyTrust(ctx, req)
	if err != nil {
		return false, toolOutcome{
			output:  fmt.Sprintf("blocked: plan-mode read-only trust approval aborted (%v)", err),
			blocked: true,
			errMsg:  fmt.Sprintf("blocked: %v", err),
		}, true
	}
	if !allow {
		if strings.TrimSpace(reason) == "" {
			reason = "the user declined to trust this MCP read-only hint — do not retry it; continue planning with other trusted read-only tools."
		}
		return false, toolOutcome{
			output:  "blocked: " + reason,
			blocked: true,
			errMsg:  "blocked by plan-mode MCP read-only trust",
		}, true
	}
	return true, toolOutcome{}, true
}

func planModeMCPTrustTarget(toolName string, t tool.Tool) (server, rawTool string, ok bool) {
	if meta, metaOK := t.(tool.MCPMetadata); metaOK {
		server = strings.TrimSpace(meta.MCPServerName())
		rawTool = strings.TrimSpace(meta.MCPRawToolName())
		if server != "" && rawTool != "" {
			return server, rawTool, true
		}
	}
	server, rawTool, ok = tool.SplitMCPName(toolName)
	return server, rawTool, ok
}

func (a *Agent) planModeBlocked(toolName string, readOnly, untrusted bool, safety planmode.PlanSafety, args json.RawMessage) (blocked bool, message string) {
	decision := planmode.Policy{AllowedTools: a.planModeAllowedTools}.Decide(planmode.Call{
		Name:      toolName,
		ReadOnly:  readOnly,
		Untrusted: untrusted,
		Safety:    safety,
		Args:      args,
	})
	return decision.Blocked, decision.Message
}

func planModeBashBlocked(args json.RawMessage) (bool, string) {
	decision := planmode.Policy{}.Decide(planmode.Call{Name: "bash", Args: args})
	return decision.Blocked, decision.Message
}

func (a *Agent) repeatedSuccessBlock(call provider.ToolCall, t tool.Tool) (string, bool) {
	sig, ok := repeatSuccessSignature(call, t)
	if !ok || a.repeatSuccessCounts == nil {
		return "", false
	}
	count := a.repeatSuccessCounts[sig]
	if count < repeatSuccessBreakThreshold {
		return "", false
	}
	return fmt.Sprintf(
		"blocked: [loop guard] %q has already succeeded %d times with the same write-like arguments in this user turn. Re-running it is unlikely to help and may burn tokens or repeat file writes. Change approach: use edit_file or multi_edit for file changes, verify with a read/test command, or explain the blocker in your final answer.",
		call.Name, count), true
}

func (a *Agent) recordRepeatSuccess(call provider.ToolCall, t tool.Tool) {
	sig, ok := repeatSuccessSignature(call, t)
	if !ok {
		return
	}
	if a.repeatSuccessCounts == nil {
		a.repeatSuccessCounts = make(map[string]int)
	}
	a.repeatSuccessCounts[sig]++
}

func repeatSuccessSignature(call provider.ToolCall, t tool.Tool) (string, bool) {
	if t.ReadOnly() {
		return "", false
	}
	switch call.Name {
	case "write_file", "edit_file", "multi_edit", "move_file", "notebook_edit":
		return call.Name + "\x00" + canonicalToolArgs(call.Arguments), true
	case "bash":
		var p struct {
			Command         string `json:"command"`
			RunInBackground bool   `json:"run_in_background"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &p); err != nil {
			return "", false
		}
		if p.RunInBackground || !isShellFileWriteCommand(p.Command) {
			return "", false
		}
		return "bash\x00" + normalizeShellCommand(p.Command), true
	default:
		return "", false
	}
}

func canonicalToolArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return strings.TrimSpace(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, b); err != nil {
		return string(b)
	}
	return compact.String()
}

func normalizeShellCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func isShellFileWriteCommand(command string) bool {
	lower := strings.ToLower(command)
	switch {
	case shellPythonOpenWrites(lower):
		return true
	case strings.Contains(lower, "set-content") || strings.Contains(lower, "add-content") || strings.Contains(lower, "out-file"):
		return true
	case strings.Contains(lower, "sed -i") || strings.Contains(lower, "perl -pi"):
		return true
	case hasShellWriteRedirect(command):
		return true
	default:
		return false
	}
}

func shellPythonOpenWrites(lower string) bool {
	if !strings.Contains(lower, "open(") {
		return false
	}
	if strings.Contains(lower, ".write(") {
		return true
	}
	for _, marker := range []string{", 'w", `, "w`, ", 'a", `, "a`, ", 'x", `, "x`, "mode='w", `mode="w`, "mode='a", `mode="a`, "mode='x", `mode="x`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasShellWriteRedirect(command string) bool {
	var quote rune
	var prev rune
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			prev = r
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			prev = r
			continue
		}
		if r == '>' {
			if prev == '2' {
				prev = r
				continue
			}
			return true
		}
		prev = r
	}
	return false
}

// isBackgroundTaskCall reports whether a `task` call set run_in_background, so a
// fire-and-return dispatch isn't mistaken for a sub-agent that has stopped.
func isBackgroundTaskCall(args string) bool {
	var p struct {
		RunInBackground bool `json:"run_in_background"`
	}
	_ = json.Unmarshal([]byte(args), &p)
	return p.RunInBackground
}

// toolReadOnly reports a tool's ReadOnly classification by name (false for an
// unknown tool), for stamping early ToolDispatch events.
func (a *Agent) toolReadOnly(name string) bool {
	t, ok := a.tools.Get(name)
	return ok && t.ReadOnly()
}

// firstLine returns s up to its first newline — a one-line failure summary for
// the display Err, while the full error stays in the model-facing output.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// truncateToolOutput head+tails s when it exceeds maxToolOutputBytes, slicing
// on rune boundaries so we never split a multibyte glyph. Returns the possibly
// trimmed body plus a one-line user-facing notice when truncation happened
// (empty when it didn't, without the "· " display prefix).
func truncateToolOutput(s string) (string, string) {
	if len(s) <= maxToolOutputBytes {
		return s, ""
	}
	keep := maxToolOutputBytes / 2
	head := snapToRuneBoundary(s, 0, keep)
	tail := snapToRuneBoundary(s, len(s)-keep, len(s))
	omitted := len(s) - len(head) - len(tail)
	notice := fmt.Sprintf("tool output truncated: %d of %d bytes elided", omitted, len(s))
	body := head + fmt.Sprintf("\n\n…[truncated %d of %d bytes — rerun with narrower args to see the middle]…\n\n", omitted, len(s)) + tail
	return body, notice
}

// snapToRuneBoundary returns s[lo:hi] with the bounds nudged outward until
// both land on rune-start positions.
func snapToRuneBoundary(s string, lo, hi int) string {
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	for hi < len(s) && !utf8.RuneStart(s[hi]) {
		hi++
	}
	return s[lo:hi]
}

// finishReasonMessage maps an abnormal finish_reason to a one-line warning,
// returning ok=false for the normal terminations ("stop", "tool_calls") and a
// nil usage. The sink renders the message; the "! " prefix is presentation.
func finishReasonMessage(u *provider.Usage) (string, bool) {
	if u == nil {
		return "", false
	}
	switch u.FinishReason {
	case "length":
		return "response truncated: hit max output tokens", true
	case "content_filter":
		return "response blocked by content filter", true
	case "repetition_truncation":
		return "response truncated: model repetition detected", true
	default:
		return "", false
	}
}
