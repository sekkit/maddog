package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"maddog/internal/provider"
)

func newResponsesClient(cfg provider.Config, name string) (provider.Provider, error) {
	keyEnv, _ := cfg.Extra["api_key_env"].(string)
	auth := provider.AuthConfigFromExtra(cfg.Extra, cfg.APIKey, keyEnv)
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("openai responses: network: %w", err)
	}
	return &responsesClient{
		name:    name,
		keyEnv:  keyEnv,
		auth:    auth,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		effort:  stringExtra(cfg.Extra, "effort"),
		http:    httpClient,
	}, nil
}

type responsesClient struct {
	name    string
	keyEnv  string
	auth    provider.AuthConfig
	baseURL string
	model   string
	effort  string
	http    *http.Client
	authed  atomic.Bool
	authMu  sync.Mutex
	authExp time.Time
}

func (c *responsesClient) Name() string { return c.name }

func (c *responsesClient) sendOpts() provider.SendOptions {
	return provider.SendOptions{
		Provider:   c.name,
		KeyEnv:     c.keyEnv,
		KeyPresent: c.auth.Token != "",
		RetryAuth:  c.authed.Load(),
	}
}

func (c *responsesClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(c.buildRequest(req)); err != nil {
		bufPool.Put(buf)
		return nil, fmt.Errorf("%s: marshal responses request: %w", c.name, err)
	}
	body := make([]byte, buf.Len())
	copy(body, buf.Bytes())
	bufPool.Put(buf)

	newReq := func(ctx context.Context) (*http.Request, error) {
		auth, err := c.requestAuth(ctx)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		auth.Header(httpReq, "Authorization")
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(ctx, c.http, c.sendOpts(), newReq)
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)
	out := make(chan provider.Chunk)
	go c.readStream(ctx, resp, out)
	return out, nil
}

func (c *responsesClient) requestAuth(ctx context.Context) (provider.AuthConfig, error) {
	auth := c.auth
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

func (c *responsesClient) exchangeWorkloadIdentity(ctx context.Context, auth provider.AuthConfig) (string, time.Time, error) {
	c.authMu.Lock()
	if c.auth.Token != "" && (c.authExp.IsZero() || time.Until(c.authExp) > time.Minute) {
		token, exp := c.auth.Token, c.authExp
		c.authMu.Unlock()
		return token, exp, nil
	}
	c.authMu.Unlock()
	return exchangeOpenAIWorkloadIdentity(ctx, c.http, c.name, auth)
}

func (c *responsesClient) buildRequest(req provider.Request) responsesRequest {
	src := provider.SanitizeToolPairing(req.Messages)
	var instructions []string
	var input []responsesInput
	for _, m := range src {
		if m.Role == provider.RoleSystem {
			if strings.TrimSpace(m.Content) != "" {
				instructions = append(instructions, m.Content)
			}
			continue
		}
		switch m.Role {
		case provider.RoleUser:
			input = append(input, responsesInput{Role: "user", Content: m.Content})
		case provider.RoleAssistant:
			if m.Content != "" {
				input = append(input, responsesInput{Role: "assistant", Content: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input = append(input, responsesInput{
					Type:      "function_call",
					CallID:    firstNonEmpty(tc.ID, tc.Name),
					Name:      tc.Name,
					Arguments: firstNonEmpty(tc.Arguments, "{}"),
				})
			}
		case provider.RoleTool:
			input = append(input, responsesInput{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: m.Content,
			})
		}
	}
	tools := make([]responsesTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	out := responsesRequest{
		Model:        c.model,
		Instructions: strings.Join(instructions, "\n\n"),
		Input:        input,
		Tools:        tools,
		Stream:       true,
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
	}
	if c.effort != "" {
		out.Reasoning = &responsesReasoning{Effort: c.effort}
	}
	return out
}

func (c *responsesClient) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk) {
	defer close(out)
	defer resp.Body.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			resp.Body.Close()
		case <-done:
		}
	}()

	acc := map[int]*provider.ToolCall{}
	started := map[int]bool{}
	var order []int

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev responsesEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: decode responses stream: %w", c.name, err)}
			return
		}
		if ev.Error != nil {
			out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: %s", c.name, ev.Error.Message)}
			return
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				out <- provider.Chunk{Type: provider.ChunkText, Text: ev.Delta}
			}
		case "response.output_item.added":
			if ev.Item.Type == "function_call" {
				tc := c.responsesToolCall(acc, &order, ev.OutputIndex, ev.Item.CallIdentifier())
				tc.Name = ev.Item.Name
				if ev.Item.CallID != "" {
					tc.ID = ev.Item.CallID
				}
				if ev.Item.Arguments != "" {
					tc.Arguments = ev.Item.Arguments
				}
				if !started[ev.OutputIndex] && tc.Name != "" {
					started[ev.OutputIndex] = true
					out <- provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: tc.ID, Name: tc.Name}}
				}
			}
		case "response.function_call_arguments.delta":
			tc := c.responsesToolCall(acc, &order, ev.OutputIndex, ev.ItemID)
			tc.Arguments += ev.Delta
		case "response.function_call_arguments.done":
			tc := c.responsesToolCall(acc, &order, ev.OutputIndex, ev.ItemID)
			if ev.Arguments != "" {
				tc.Arguments = ev.Arguments
			}
		case "response.completed":
			if ev.Response != nil && ev.Response.Usage != nil {
				u := normaliseResponsesUsage(ev.Response.Usage)
				u.FinishReason = ev.Response.Status
				out <- provider.Chunk{Type: provider.ChunkUsage, Usage: u}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("%s: read responses stream: %w", c.name, err)}
		return
	}

	sort.Ints(order)
	for _, idx := range order {
		tc := acc[idx]
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d", idx)
		}
		out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
}

func (c *responsesClient) responsesToolCall(acc map[int]*provider.ToolCall, order *[]int, idx int, fallbackID string) *provider.ToolCall {
	tc, ok := acc[idx]
	if !ok {
		tc = &provider.ToolCall{ID: fallbackID}
		acc[idx] = tc
		*order = append(*order, idx)
	}
	if tc.ID == "" {
		tc.ID = fallbackID
	}
	return tc
}

func normaliseResponsesUsage(u *responsesUsage) *provider.Usage {
	hit := 0
	if u.InputTokensDetails != nil {
		hit = u.InputTokensDetails.CachedTokens
	}
	miss := 0
	if u.InputTokens > hit {
		miss = u.InputTokens - hit
	}
	reasoning := 0
	if u.OutputTokensDetails != nil {
		reasoning = u.OutputTokensDetails.ReasoningTokens
	}
	return &provider.Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
		CacheHitTokens:   hit,
		CacheMissTokens:  miss,
		ReasoningTokens:  reasoning,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func stringExtra(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	v, _ := extra[key].(string)
	return strings.TrimSpace(v)
}

type responsesRequest struct {
	Model        string              `json:"model"`
	Instructions string              `json:"instructions,omitempty"`
	Input        []responsesInput    `json:"input"`
	Tools        []responsesTool     `json:"tools,omitempty"`
	Reasoning    *responsesReasoning `json:"reasoning,omitempty"`
	Stream       bool                `json:"stream"`
	Temperature  float64             `json:"temperature,omitempty"`
	MaxTokens    int                 `json:"max_output_tokens,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesInput struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type responsesEvent struct {
	Type        string        `json:"type"`
	Delta       string        `json:"delta"`
	OutputIndex int           `json:"output_index"`
	ItemID      string        `json:"item_id"`
	Arguments   string        `json:"arguments"`
	Item        responsesItem `json:"item"`
	Response    *struct {
		Status string          `json:"status"`
		Usage  *responsesUsage `json:"usage"`
	} `json:"response"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type responsesItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (i responsesItem) CallIdentifier() string {
	if i.CallID != "" {
		return i.CallID
	}
	return i.ID
}

type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}
