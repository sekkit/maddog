// Package anthropic implements the Anthropic Messages API provider (POST
// /v1/messages, SSE streaming) with a hand-written net/http client — no SDK. It
// self-registers under the "anthropic" kind, so any Claude model is a config
// instance rather than code.
//
// Two notes, both rooted in the transport-agnostic provider.Message abstraction:
//
//   - Extended thinking is opt-in (provider config thinking="adaptive"). Anthropic
//     requires the *signed* thinking block be replayed on the next turn when a tool
//     call followed thinking, so Message carries ReasoningSignature alongside
//     ReasoningContent and this provider replays the signed block on the next
//     request. Off by default because the field is Anthropic-specific — an
//     OpenAI-compatible gateway (e.g. DeepSeek's) would reject it. (redacted_thinking
//     blocks are not yet captured/replayed.)
//   - No temperature/top_p. Current Claude models (Opus 4.8/4.7) reject sampling
//     parameters with a 400; Anthropic steers behavior via prompting instead.
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"maddog/internal/netclient"
	"maddog/internal/provider"
)

// defaultStreamIdleTimeout caps how long a started SSE stream may go silent before
// it's treated as a dropped connection — a half-open TCP connection (proxy switched
// mid-stream) sends no RST, so scanner.Scan() would block forever. Generous on
// purpose; live streams emit far more often. Stored per-client (client.idleTimeout)
// so a test can shorten it without a shared global that races other watchdogs.
const defaultStreamIdleTimeout = 120 * time.Second

const (
	// anthropicVersion is the required API version header value.
	anthropicVersion = "2023-06-01"
	// advisorBetaHeader enables Anthropic's advisor server-side tool beta.
	advisorBetaHeader = "advisor-tool-2026-03-01"
	// defaultBaseURL is the first-party endpoint; config may override it (e.g. a
	// gateway). Bedrock/Vertex use a different request shape and are out of scope.
	defaultBaseURL = "https://api.anthropic.com"
	// defaultMaxTokens is the output ceiling used when the request leaves MaxTokens
	// unset. Anthropic *requires* max_tokens, and the agent currently doesn't set
	// it, so this is the de-facto cap. Generous (you only pay for tokens actually
	// produced) and within every catalog model's limit (Sonnet/Haiku 64K, Opus 128K).
	defaultMaxTokens = 32768
)

func init() {
	provider.Register("anthropic", New)
}

// New builds an Anthropic provider from a resolved config.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("anthropic: model is required for provider %q", cfg.Name)
	}
	name := cfg.Name
	if name == "" {
		name = "anthropic"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	keyEnv, _ := cfg.Extra["api_key_env"].(string) // for actionable auth errors
	auth := provider.AuthConfigFromExtra(cfg.Extra, cfg.APIKey, keyEnv)
	thinking, _ := cfg.Extra["thinking"].(string)
	effort, _ := cfg.Extra["effort"].(string)
	vision, _ := cfg.Extra["vision"].(bool)
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("anthropic: network: %w", err)
	}
	// Anthropic's API surface is at {root}/v1/messages, so c.baseURL stores
	// the *root* — without any trailing /v1. The setup wizard, however, lets
	// users paste a full OpenAI-compatible URL (e.g.
	// "https://proxy.example.com/v1") because that's what /models probes
	// expect. Stripping the trailing /v1 here makes both forms land on the
	// same endpoint without forcing users to remember Anthropic's quirky
	// root-vs-versioned split. Without this, a user pasting
	// "https://proxy.example.com/v1" would probe /v1/models successfully
	// but get the chat client concatenating onto
	// "https://proxy.example.com/v1/v1/messages" — a 404.
	root := strings.TrimRight(baseURL, "/")
	root = strings.TrimSuffix(root, "/v1")
	if root == "" {
		root = defaultBaseURL
	}
	return &client{
		name:        name,
		apiKey:      cfg.APIKey,
		keyEnv:      keyEnv,
		auth:        auth,
		baseURL:     root,
		model:       cfg.Model,
		thinking:    thinking,
		effort:      effort,
		vision:      vision,
		idleTimeout: defaultStreamIdleTimeout,
		http:        httpClient, // no overall timeout; lifecycle is ctx-driven
	}, nil
}

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	return netclient.NewHTTPClient(spec, netclient.TransportOptions{})
}

type client struct {
	name        string
	apiKey      string
	keyEnv      string // api_key_env name, surfaced in auth errors
	auth        provider.AuthConfig
	baseURL     string
	model       string
	thinking    string // "adaptive" enables extended thinking; "" = off (config-driven)
	effort      string // output_config.effort: low|medium|high|xhigh|max; "" = provider default
	vision      bool
	http        *http.Client
	authed      atomic.Bool
	idleTimeout time.Duration
	authMu      sync.Mutex
	authExp     time.Time
}

func (c *client) Name() string { return c.name }

func (c *client) sendOpts() provider.SendOptions {
	return provider.SendOptions{
		Provider:   c.name,
		KeyEnv:     c.keyEnv,
		KeyPresent: c.apiKey != "",
		RetryAuth:  c.authed.Load(),
	}
}

// bufPool reuses byte buffers for JSON-marshalled request bodies, reducing GC
// churn from repeated alloc/free of ~10-100KB buffers per turn.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(c.buildRequest(req)); err != nil {
		bufPool.Put(buf)
		return nil, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}
	body := make([]byte, buf.Len())
	copy(body, buf.Bytes())
	bufPool.Put(buf)

	newReq := func(ctx context.Context) (*http.Request, error) {
		auth, err := c.requestAuth(ctx)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		auth.Header(httpReq, "x-api-key")
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		if req.NativeAdvisor != nil {
			httpReq.Header.Set("anthropic-beta", advisorBetaHeader)
		}
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(ctx, c.http, c.sendOpts(), newReq)
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk)
	go c.readStream(resp, out)
	return out, nil
}

func (c *client) requestAuth(ctx context.Context) (provider.AuthConfig, error) {
	auth := c.auth
	if auth.Token == "" {
		auth.Token = c.apiKey
	}
	if auth.TokenEnv == "" {
		auth.TokenEnv = c.keyEnv
	}
	if auth.NormalizedType() != provider.AuthTypeWorkloadIdentity || auth.Token != "" {
		return auth, nil
	}
	token, exp, err := c.exchangeWorkloadIdentity(ctx, auth)
	if err != nil {
		return auth, err
	}
	auth.Token = token
	c.authMu.Lock()
	c.auth.Token = token
	c.authExp = exp
	c.authMu.Unlock()
	return auth, nil
}

func (c *client) exchangeWorkloadIdentity(ctx context.Context, auth provider.AuthConfig) (string, time.Time, error) {
	c.authMu.Lock()
	if c.auth.Token != "" && (c.authExp.IsZero() || time.Until(c.authExp) > time.Minute) {
		token, exp := c.auth.Token, c.authExp
		c.authMu.Unlock()
		return token, exp, nil
	}
	c.authMu.Unlock()

	assertion := strings.TrimSpace(auth.IdentityToken)
	if assertion == "" {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth requires identity token", c.name)
	}
	body := map[string]string{
		"grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
		"assertion":  assertion,
	}
	for _, key := range []string{"federation_rule_id", "organization_id", "service_account_id", "workspace_id"} {
		if v := strings.TrimSpace(auth.Extra[key]); v != "" {
			body[key] = v
		}
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth: marshal token request: %w", c.name, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/oauth/token", &buf)
	if err != nil {
		return "", time.Time{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth: token exchange failed: %w", c.name, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth: read token response: %w", c.name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth: token exchange status %d: %s", c.name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth: decode token response: %w", c.name, err)
	}
	token := strings.TrimSpace(decoded.AccessToken)
	if token == "" {
		return "", time.Time{}, fmt.Errorf("%s: workload identity auth: token response missing access_token", c.name)
	}
	var exp time.Time
	if decoded.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second)
	}
	return token, exp, nil
}

// buildRequest converts the transport-agnostic Request into the Messages API shape:
// RoleSystem messages lift to the top-level `system` field; assistant tool calls
// become `tool_use` blocks; RoleTool results become `tool_result` blocks in a user
// turn. Consecutive same-role messages are coalesced because the API requires
// alternating user/assistant turns (tool results are user turns).
func (c *client) buildRequest(req provider.Request) anthRequest {
	var system []textBlock
	var msgs []anthMessage

	// appendBlocks adds blocks under role, merging into the previous message when
	// it shares the role (keeps user/assistant strictly alternating).
	appendBlocks := func(role string, blocks ...contentBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Content = append(msgs[n-1].Content, blocks...)
			return
		}
		msgs = append(msgs, anthMessage{Role: role, Content: blocks})
	}

	for _, m := range provider.SanitizeToolPairing(req.Messages) {
		switch m.Role {
		case provider.RoleSystem:
			if m.Content != "" {
				system = append(system, textBlock{Type: "text", Text: m.Content})
			}
		case provider.RoleUser:
			if m.Content != "" {
				appendBlocks("user", contentBlock{Type: "text", Text: m.Content})
			}
			if c.vision {
				for _, url := range m.Images {
					if mt, data, ok := provider.ParseImageDataURL(url); ok {
						appendBlocks("user", contentBlock{Type: "image", Source: &imageSource{Type: "base64", MediaType: mt, Data: data}})
					}
				}
			}
		case provider.RoleTool:
			content := m.Content
			if content == "" {
				content = "(no output)" // tool_result content must be non-empty
			}
			appendBlocks("user", contentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: content})
		case provider.RoleAssistant:
			if len(m.NativeBlocks) > 0 && req.NativeAdvisor != nil {
				appendBlocks("assistant", nativeContentBlocks(m.NativeBlocks)...)
				continue
			}
			var blocks []contentBlock
			// Replay the signed thinking block first (Anthropic requires it precede
			// the tool_use it led to). Only when thinking is on and we have both the
			// text and its signature — reasoning without a signature (e.g. from an
			// openai-compatible provider) can't be replayed as a thinking block.
			if c.thinking != "" && m.ReasoningContent != "" && m.ReasoningSignature != "" {
				blocks = append(blocks, contentBlock{Type: "thinking", Thinking: m.ReasoningContent, Signature: m.ReasoningSignature})
			}
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}") // input is required, even when empty
				}
				blocks = append(blocks, contentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
			}
			appendBlocks("assistant", blocks...)
		}
	}

	var tools []anthTool
	for _, t := range req.Tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, anthTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	if na := req.NativeAdvisor; na != nil {
		maxTokens := na.MaxTokens
		if maxTokens > 0 && maxTokens < 1024 {
			maxTokens = 1024
		}
		tools = append(tools, anthTool{
			Type:      "advisor_20260301",
			Name:      "advisor",
			Model:     na.Model,
			MaxUses:   na.MaxUses,
			MaxTokens: maxTokens,
		})
	}

	// Prompt-cache breakpoints (ephemeral, prefix-match). Render order is
	// tools → system → messages, so a marker on the last system block caches
	// tools+system together; with no system, mark the last tool. A marker on the
	// last block of the last message caches the conversation prefix, accruing hits
	// incrementally as turns are appended. Max 4 breakpoints; we use ≤2.
	if n := len(system); n > 0 {
		system[n-1].CacheControl = ephemeral()
	} else if i := lastCacheableToolIndex(tools); i >= 0 {
		tools[i].CacheControl = ephemeral()
	}
	if n := len(msgs); n > 0 {
		if k := len(msgs[n-1].Content); k > 0 {
			msgs[n-1].Content[k-1].CacheControl = ephemeral()
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	r := anthRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
		Tools:     tools,
		Stream:    true,
	}
	// Extended thinking is opt-in and Anthropic-specific (a compatible gateway like
	// DeepSeek's would reject the field). "summarized" display streams the reasoning
	// text; the default omits it but still emits the signature we round-trip.
	if c.thinking == "adaptive" {
		r.Thinking = &thinkingConfig{Type: "adaptive", Display: "summarized"}
		if c.effort != "" {
			r.OutputConfig = &outputConfig{Effort: c.effort}
		}
	}
	return r
}

func nativeContentBlocks(raw []json.RawMessage) []contentBlock {
	blocks := make([]contentBlock, 0, len(raw))
	for _, rb := range raw {
		if len(rb) == 0 {
			continue
		}
		var block contentBlock
		if err := json.Unmarshal(rb, &block); err != nil {
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// readStream parses the Messages API SSE stream into Chunks. Text deltas emit live;
// each tool_use content block emits a ChunkToolCallStart when its id+name are known
// and a complete ChunkToolCall when the block closes; usage is assembled from
// message_start (input/cache) + message_delta (output + stop_reason) and emitted
// once before ChunkDone.
func (c *client) readStream(resp *http.Response, out chan<- provider.Chunk) {
	defer resp.Body.Close()
	defer close(out)

	// Close the body if the stream stalls past c.idleTimeout so scanner.Scan()
	// unblocks instead of hanging on a half-open connection. The watchdog owns the
	// timer; the read loop only pings the buffered activity channel (no Timer.Reset
	// race). A context cancel already unblocks the scan via the transport.
	idleTimeout := c.idleTimeout
	if idleTimeout <= 0 { // zero-value client (constructed without New)
		idleTimeout = defaultStreamIdleTimeout
	}
	done := make(chan struct{})
	defer close(done)
	activity := make(chan struct{}, 1)
	var stalled atomic.Bool
	go func() {
		idle := time.NewTimer(idleTimeout)
		defer idle.Stop()
		for {
			select {
			case <-idle.C:
				stalled.Store(true)
				resp.Body.Close()
				return
			case <-activity:
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(idleTimeout)
			case <-done:
				return
			}
		}
	}()

	tools := map[int]*provider.ToolCall{} // tool_use blocks, keyed by content index
	textBlocks := map[int]*strings.Builder{}
	serverBlocks := map[int]*wireContentBlock{}
	var nativeBlocks []json.RawMessage
	preserveNative := false
	var inTok, outTok, cacheCreate, cacheRead int
	var stopReason string
	haveUsage := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select { // ping the idle watchdog; non-blocking so a full buffer is fine
		case activity <- struct{}{}:
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		// SSE carries `event:` and `data:` lines; the data JSON's own `type` field
		// is authoritative, so we only need the data payloads.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		var ev streamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: decode stream: %w", c.name, err)}
			return
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				inTok = ev.Message.Usage.InputTokens
				cacheCreate = ev.Message.Usage.CacheCreationInputTokens
				cacheRead = ev.Message.Usage.CacheReadInputTokens
				haveUsage = true
			}
		case "content_block_start":
			if ev.ContentBlock == nil {
				continue
			}
			switch ev.ContentBlock.Type {
			case "text":
				textBlocks[ev.Index] = &strings.Builder{}
			case "tool_use":
				tc := &provider.ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
				tools[ev.Index] = tc
				out <- provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}}
			case "server_tool_use":
				preserveNative = true
				serverBlocks[ev.Index] = ev.ContentBlock
			case "advisor_tool_result":
				preserveNative = true
				if block := marshalNativeBlock(ev.ContentBlock); len(block) > 0 {
					nativeBlocks = append(nativeBlocks, block)
				}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					if b := textBlocks[ev.Index]; b != nil {
						b.WriteString(ev.Delta.Text)
					}
					out <- provider.Chunk{Type: provider.ChunkText, Text: ev.Delta.Text}
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					out <- provider.Chunk{Type: provider.ChunkReasoning, Text: ev.Delta.Thinking}
				}
			case "signature_delta":
				if ev.Delta.Signature != "" {
					out <- provider.Chunk{Type: provider.ChunkReasoning, Signature: ev.Delta.Signature}
				}
			case "input_json_delta":
				if tc := tools[ev.Index]; tc != nil {
					tc.Arguments += ev.Delta.PartialJSON
				}
			}
		case "content_block_stop":
			if b := textBlocks[ev.Index]; b != nil {
				if block := marshalTextNativeBlock(b.String()); len(block) > 0 {
					nativeBlocks = append(nativeBlocks, block)
				}
				delete(textBlocks, ev.Index)
			}
			if tc := tools[ev.Index]; tc != nil {
				if block := marshalToolUseNativeBlock(tc); len(block) > 0 {
					nativeBlocks = append(nativeBlocks, block)
				}
				out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}
				delete(tools, ev.Index)
			}
			if block := serverBlocks[ev.Index]; block != nil {
				if raw := marshalNativeBlock(block); len(raw) > 0 {
					nativeBlocks = append(nativeBlocks, raw)
				}
				delete(serverBlocks, ev.Index)
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				outTok = ev.Usage.OutputTokens
				haveUsage = true
			}
		case "message_stop":
			// Stream complete; fall through to finalize below.
		case "error":
			msg := "stream error"
			errType := ""
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			if ev.Error != nil {
				errType = ev.Error.Type
			}
			out <- provider.Chunk{Type: provider.ChunkError, Err: provider.NewStructuredAPIError(c.name, errType, "", msg)}
			return
		}
	}

	if stalled.Load() {
		out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped", c.name, idleTimeout)}
		return
	}
	if err := scanner.Err(); err != nil {
		out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: read stream: %w", c.name, err)}
		return
	}

	if preserveNative {
		for _, block := range nativeBlocks {
			out <- provider.Chunk{Type: provider.ChunkNativeBlock, NativeBlock: block}
		}
	}
	if haveUsage {
		out <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     inTok + cacheCreate + cacheRead,
			CompletionTokens: outTok,
			TotalTokens:      inTok + cacheCreate + cacheRead + outTok,
			CacheHitTokens:   cacheRead,
			CacheMissTokens:  inTok + cacheCreate, // uncached input + cache writes (billed ≥1×)
			FinishReason:     mapStopReason(stopReason),
		}}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
}

// mapStopReason translates Anthropic stop reasons to the OpenAI-style finish
// reasons the agent already recognises (it surfaces abnormal ones like "length").
func mapStopReason(s string) string {
	switch s {
	case "end_turn", "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return s // "refusal", "pause_turn", "" — pass through
	}
}

// --- Messages API wire protocol ---

func ephemeral() *cacheControl { return &cacheControl{Type: "ephemeral"} }

type cacheControl struct {
	Type string `json:"type"`
}

type anthRequest struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	System       []textBlock     `json:"system,omitempty"`
	Messages     []anthMessage   `json:"messages"`
	Tools        []anthTool      `json:"tools,omitempty"`
	Thinking     *thinkingConfig `json:"thinking,omitempty"`
	OutputConfig *outputConfig   `json:"output_config,omitempty"`
	Stream       bool            `json:"stream"`
}

type thinkingConfig struct {
	Type    string `json:"type"`              // "adaptive"
	Display string `json:"display,omitempty"` // "summarized" to stream the reasoning text
}

type outputConfig struct {
	Effort string `json:"effort,omitempty"` // low | medium | high | xhigh | max
}

type textBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type anthMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

// contentBlock is the union of the block kinds we emit in a request: text,
// tool_use (echoing a prior assistant call), and tool_result. Unused fields are
// omitted so each block serialises to its canonical shape.
type contentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`        // text
	Thinking     string          `json:"thinking,omitempty"`    // thinking
	Signature    string          `json:"signature,omitempty"`   // thinking
	ID           string          `json:"id,omitempty"`          // tool_use
	Name         string          `json:"name,omitempty"`        // tool_use / server_tool_use
	Input        json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID    string          `json:"tool_use_id,omitempty"` // tool_result
	Content      any             `json:"content,omitempty"`     // tool_result / server tool result
	Source       *imageSource    `json:"source,omitempty"`      // image
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthTool struct {
	Name         string          `json:"name"`
	Type         string          `json:"type,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	Model        string          `json:"model,omitempty"`
	MaxUses      int             `json:"max_uses,omitempty"`
	MaxTokens    int             `json:"max_tokens,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

func lastCacheableToolIndex(tools []anthTool) int {
	for i := len(tools) - 1; i >= 0; i-- {
		if tools[i].Type == "" {
			return i
		}
	}
	return -1
}

// streamEvent is the discriminated SSE event; read the fields matching Type.
type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *wireContentBlock `json:"content_block"`
	Delta        *struct {
		Type        string `json:"type"`         // text_delta | thinking_delta | signature_delta | input_json_delta
		Text        string `json:"text"`         // text_delta
		Thinking    string `json:"thinking"`     // thinking_delta
		Signature   string `json:"signature"`    // signature_delta
		PartialJSON string `json:"partial_json"` // input_json_delta
		StopReason  string `json:"stop_reason"`  // message_delta
	} `json:"delta"`
	Usage *wireUsage `json:"usage"` // message_delta (cumulative output_tokens)
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type wireContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Text      string          `json:"text,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

func marshalTextNativeBlock(text string) json.RawMessage {
	if text == "" {
		return nil
	}
	return marshalNativeBlock(&wireContentBlock{Type: "text", Text: text})
}

func marshalToolUseNativeBlock(tc *provider.ToolCall) json.RawMessage {
	if tc == nil {
		return nil
	}
	input := json.RawMessage(tc.Arguments)
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	return marshalNativeBlock(&wireContentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
}

func marshalNativeBlock(block *wireContentBlock) json.RawMessage {
	if block == nil || block.Type == "" {
		return nil
	}
	if (block.Type == "server_tool_use" || block.Type == "tool_use") && len(block.Input) == 0 {
		block = cloneWireContentBlock(block)
		block.Input = json.RawMessage("{}")
	}
	b, err := json.Marshal(block)
	if err != nil {
		return nil
	}
	return b
}

func cloneWireContentBlock(block *wireContentBlock) *wireContentBlock {
	if block == nil {
		return nil
	}
	cp := *block
	if len(block.Input) > 0 {
		cp.Input = append(json.RawMessage(nil), block.Input...)
	}
	if len(block.Content) > 0 {
		cp.Content = append(json.RawMessage(nil), block.Content...)
	}
	return &cp
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}
