package serve

import "maddog/internal/event"

// wireEvent is the JSON shape an event.Event takes on the SSE stream. It uses
// explicit lowercase tags (a clean contract for a JS client) and flattens the
// few non-JSON-friendly bits — the Kind enum becomes a string, the TurnDone
// error becomes a message — so a browser frontend renders the same typed stream
// the TUI does.
type wireEvent struct {
	Kind           string              `json:"kind"`
	Text           string              `json:"text,omitempty"`
	Reasoning      string              `json:"reasoning,omitempty"`
	Level          string              `json:"level,omitempty"`
	Tool           *wireTool           `json:"tool,omitempty"`
	Usage          *wireUsage          `json:"usage,omitempty"`
	ProviderStatus *wireProviderStatus `json:"providerStatus,omitempty"`
	Advisor        *wireAdvisor        `json:"advisor,omitempty"`
	Approval       *wireApproval       `json:"approval,omitempty"`
	Ask            *wireAsk            `json:"ask,omitempty"`
	Compaction     *wireCompaction     `json:"compaction,omitempty"`
	Err            string              `json:"err,omitempty"`
	RetryAttempt   int                 `json:"retryAttempt,omitempty"`
	RetryMax       int                 `json:"retryMax,omitempty"`
}

// wireCompaction is the JSON form of an event.Compaction. On a compaction_started
// event only Trigger is set; compaction_done carries the rest (an aborted pass
// leaves Summary empty so the frontend drops its placeholder).
type wireCompaction struct {
	Trigger  string `json:"trigger,omitempty"`
	Messages int    `json:"messages,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Archive  string `json:"archive,omitempty"`
}

type wireAskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type wireAskQuestion struct {
	ID      string          `json:"id"`
	Header  string          `json:"header,omitempty"`
	Prompt  string          `json:"prompt"`
	Options []wireAskOption `json:"options"`
	Multi   bool            `json:"multi,omitempty"`
}

type wireAsk struct {
	ID        string            `json:"id"`
	Questions []wireAskQuestion `json:"questions"`
}

type wireProfile struct {
	Role            string `json:"role,omitempty"`
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`
	BudgetUsed      int64  `json:"budgetUsed,omitempty"`
	BudgetLimit     int64  `json:"budgetLimit,omitempty"`
	BudgetRemaining int64  `json:"budgetRemaining,omitempty"`
}

type wireProviderStatus struct {
	Role             string `json:"role,omitempty"`
	Health           string `json:"health,omitempty"`
	AuthStatus       string `json:"authStatus,omitempty"`
	RateLimit        string `json:"rateLimit,omitempty"`
	BalanceStatus    string `json:"balanceStatus,omitempty"`
	LastError        string `json:"lastError,omitempty"`
	BalanceAvailable bool   `json:"balanceAvailable,omitempty"`
	BalanceDisplay   string `json:"balanceDisplay,omitempty"`
}

type wireTool struct {
	ID          string           `json:"id,omitempty"`
	Name        string           `json:"name"`
	Args        string           `json:"args,omitempty"`
	Output      string           `json:"output,omitempty"`
	Err         string           `json:"err,omitempty"`
	ReadOnly    bool             `json:"readOnly"`
	Truncated   bool             `json:"truncated,omitempty"`
	DurationMs  int64            `json:"durationMs,omitempty"`
	Partial     bool             `json:"partial,omitempty"`
	ParentID    string           `json:"parentId,omitempty"`
	Profile     *wireProfile     `json:"profile,omitempty"`
	Compression *wireCompression `json:"compression,omitempty"`
}

type wireCompression struct {
	RawRef           string   `json:"rawRef,omitempty"`
	Route            string   `json:"route,omitempty"`
	Profile          string   `json:"profile,omitempty"`
	Quality          string   `json:"quality,omitempty"`
	QualityReason    string   `json:"qualityReason,omitempty"`
	UnparsedLines    int      `json:"unparsedLines,omitempty"`
	UnparsedSamples  []string `json:"unparsedSamples,omitempty"`
	Lossy            bool     `json:"lossy,omitempty"`
	OmittedLines     int      `json:"omittedLines,omitempty"`
	Strategy         string   `json:"strategy,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	RawChars         int      `json:"rawChars,omitempty"`
	CompressedChars  int      `json:"compressedChars,omitempty"`
	SavedChars       int      `json:"savedChars,omitempty"`
	RawTokens        int      `json:"rawTokens,omitempty"`
	CompressedTokens int      `json:"compressedTokens,omitempty"`
	SavedTokens      int      `json:"savedTokens,omitempty"`
}

type wireUsage struct {
	PromptTokens     int                   `json:"promptTokens"`
	CompletionTokens int                   `json:"completionTokens"`
	TotalTokens      int                   `json:"totalTokens"`
	CacheHitTokens   int                   `json:"cacheHitTokens"`
	CacheMissTokens  int                   `json:"cacheMissTokens"`
	ReasoningTokens  int                   `json:"reasoningTokens,omitempty"`
	Profile          *wireProfile          `json:"profile,omitempty"`
	ProviderStatus   *wireProviderStatus   `json:"providerStatus,omitempty"`
	CacheDiagnostics *wireCacheDiagnostics `json:"cacheDiagnostics,omitempty"`
	// Session-cumulative cache tokens — the status line shows the aggregate
	// hit-rate Σhit/Σ(hit+miss), steadier than the single-turn CacheHitTokens.
	SessionCacheHitTokens  int     `json:"sessionCacheHitTokens"`
	SessionCacheMissTokens int     `json:"sessionCacheMissTokens"`
	Cost                   float64 `json:"cost,omitempty"`
	Currency               string  `json:"currency,omitempty"`
	// CostUSD is kept for older status consumers. It mirrors Cost and does not
	// imply USD.
	CostUSD float64 `json:"costUsd,omitempty"`
}

type wireCacheDiagnostics struct {
	PrefixHash          string   `json:"prefixHash"`
	PrefixChanged       bool     `json:"prefixChanged"`
	PrefixChangeReasons []string `json:"prefixChangeReasons,omitempty"`
	SystemHash          string   `json:"systemHash"`
	ToolsHash           string   `json:"toolsHash"`
	LogRewriteVersion   int      `json:"logRewriteVersion"`
	ToolSchemaTokens    int      `json:"toolSchemaTokens"`
	CacheMissTokens     int      `json:"cacheMissTokens"`
	CacheHitTokens      int      `json:"cacheHitTokens"`
}

type wireApproval struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Subject string `json:"subject"`
}

type wireAdvisor struct {
	Reason               string `json:"reason,omitempty"`
	Question             string `json:"question,omitempty"`
	Advice               string `json:"advice,omitempty"`
	UsesThisTurn         int    `json:"usesThisTurn,omitempty"`
	UsesThisSession      int    `json:"usesThisSession,omitempty"`
	RemainingThisTurn    int    `json:"remainingThisTurn,omitempty"`
	RemainingThisSession int    `json:"remainingThisSession,omitempty"`
	MaxUsesPerTurn       int    `json:"maxUsesPerTurn,omitempty"`
	MaxUsesPerSession    int    `json:"maxUsesPerSession,omitempty"`
}

// kindNames maps the event.Kind enum to stable wire strings.
var kindNames = map[event.Kind]string{
	event.TurnStarted:          "turn_started",
	event.Reasoning:            "reasoning",
	event.Text:                 "text",
	event.Message:              "message",
	event.ToolDispatch:         "tool_dispatch",
	event.ToolResult:           "tool_result",
	event.Usage:                "usage",
	event.Notice:               "notice",
	event.Phase:                "phase",
	event.ApprovalRequest:      "approval_request",
	event.AskRequest:           "ask_request",
	event.TurnDone:             "turn_done",
	event.CompactionStarted:    "compaction_started",
	event.CompactionDone:       "compaction_done",
	event.ToolProgress:         "tool_progress",
	event.MCPSurfaceReady:      "mcp_surface_ready",
	event.Retrying:             "retrying",
	event.Steer:                "steer",
	event.Upgrade:              "upgrade",
	event.SkillGenerated:       "skill_generated",
	event.BudgetExceeded:       "budget_exceeded",
	event.SkillPromoted:        "skill_promoted",
	event.Advisor:              "advisor",
	event.ProviderStatusUpdate: "provider_status",
}

// toWireAsk converts an event.Ask into its JSON wire form.
func toWireAsk(a event.Ask) *wireAsk {
	qs := make([]wireAskQuestion, len(a.Questions))
	for i, q := range a.Questions {
		opts := make([]wireAskOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = wireAskOption{Label: o.Label, Description: o.Description}
		}
		qs[i] = wireAskQuestion{ID: q.ID, Header: q.Header, Prompt: q.Prompt, Options: opts, Multi: q.Multi}
	}
	return &wireAsk{ID: a.ID, Questions: qs}
}

// toWire converts an event.Event into its JSON wire form.
func toWire(e event.Event) wireEvent {
	w := wireEvent{Kind: kindNames[e.Kind], Text: e.Text, Reasoning: e.Reasoning}
	switch e.Kind {
	case event.Notice, event.MCPSurfaceReady, event.Upgrade, event.SkillGenerated, event.BudgetExceeded, event.SkillPromoted, event.ProviderStatusUpdate:
		if e.Level == event.LevelWarn {
			w.Level = "warn"
		} else {
			w.Level = "info"
		}
		if e.Kind == event.ProviderStatusUpdate {
			w.ProviderStatus = toWireProviderStatus(e.ProviderStatus)
		}
	case event.Advisor:
		if e.Level == event.LevelWarn {
			w.Level = "warn"
		} else {
			w.Level = "info"
		}
		w.Advisor = toWireAdvisor(e.Advisor)
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		wt := &wireTool{
			ID: e.Tool.ID, Name: e.Tool.Name, Args: e.Tool.Args,
			Output: e.Tool.Output, Err: e.Tool.Err,
			ReadOnly: e.Tool.ReadOnly, Truncated: e.Tool.Truncated,
			DurationMs: e.Tool.DurationMs, Partial: e.Tool.Partial,
			ParentID: e.Tool.ParentID,
		}
		if e.Tool.Profile != nil {
			wt.Profile = toWireProfile(e.Tool.Profile)
		}
		if e.Tool.Compression != nil {
			wt.Compression = toWireCompression(e.Tool.Compression)
		}
		w.Tool = wt
	case event.Usage:
		if u := e.Usage; u != nil {
			w.Usage = &wireUsage{
				PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
				TotalTokens: u.TotalTokens, CacheHitTokens: u.CacheHitTokens,
				CacheMissTokens: u.CacheMissTokens, ReasoningTokens: u.ReasoningTokens,
				SessionCacheHitTokens: e.SessionHit, SessionCacheMissTokens: e.SessionMiss,
			}
			w.Usage.Profile = toWireProfile(e.Profile)
			w.Usage.ProviderStatus = toWireProviderStatus(e.ProviderStatus)
			if e.CacheDiagnostics != nil {
				w.Usage.CacheDiagnostics = toWireCacheDiagnostics(e.CacheDiagnostics)
			}
			if e.Pricing != nil {
				cost := e.Pricing.Cost(u)
				w.Usage.Cost = cost
				w.Usage.Currency = e.Pricing.Symbol()
				w.Usage.CostUSD = cost
			}
		}
	case event.ApprovalRequest:
		w.Approval = &wireApproval{ID: e.Approval.ID, Tool: e.Approval.Tool, Subject: e.Approval.Subject}
	case event.AskRequest:
		w.Ask = toWireAsk(e.Ask)
	case event.CompactionStarted, event.CompactionDone:
		w.Compaction = &wireCompaction{
			Trigger: e.Compaction.Trigger, Messages: e.Compaction.Messages,
			Summary: e.Compaction.Summary, Archive: e.Compaction.Archive,
		}
	case event.TurnDone:
		if e.Err != nil {
			w.Err = e.Err.Error()
		}
	}
	return w
}

func toWireProfile(p *event.Profile) *wireProfile {
	if p == nil {
		return nil
	}
	return &wireProfile{
		Role:            p.Role,
		Model:           p.Model,
		Effort:          p.Effort,
		BudgetUsed:      p.BudgetUsed,
		BudgetLimit:     p.BudgetLimit,
		BudgetRemaining: p.BudgetRemaining,
	}
}

func toWireProviderStatus(s *event.ProviderStatus) *wireProviderStatus {
	if s == nil {
		return nil
	}
	return &wireProviderStatus{
		Role:             s.Role,
		Health:           s.Health,
		AuthStatus:       s.AuthStatus,
		RateLimit:        s.RateLimit,
		BalanceStatus:    s.BalanceStatus,
		LastError:        s.LastError,
		BalanceAvailable: s.BalanceAvailable,
		BalanceDisplay:   s.BalanceDisplay,
	}
}

func toWireCompression(c *event.Compression) *wireCompression {
	if c == nil {
		return nil
	}
	return &wireCompression{
		RawRef:           c.RawRef,
		Route:            c.Route,
		Profile:          c.Profile,
		Quality:          c.Quality,
		QualityReason:    c.QualityReason,
		UnparsedLines:    c.UnparsedLines,
		UnparsedSamples:  append([]string(nil), c.UnparsedSamples...),
		Lossy:            c.Lossy,
		OmittedLines:     c.OmittedLines,
		Strategy:         c.Strategy,
		Summary:          c.Summary,
		RawChars:         c.RawChars,
		CompressedChars:  c.CompressedChars,
		SavedChars:       c.SavedChars,
		RawTokens:        c.RawTokens,
		CompressedTokens: c.CompressedTokens,
		SavedTokens:      c.SavedTokens,
	}
}

func toWireAdvisor(a event.AdvisorConsultation) *wireAdvisor {
	return &wireAdvisor{
		Reason:               a.Reason,
		Question:             a.Question,
		Advice:               a.Advice,
		UsesThisTurn:         a.UsesThisTurn,
		UsesThisSession:      a.UsesThisSession,
		RemainingThisTurn:    a.RemainingThisTurn,
		RemainingThisSession: a.RemainingThisSession,
		MaxUsesPerTurn:       a.MaxUsesPerTurn,
		MaxUsesPerSession:    a.MaxUsesPerSession,
	}
}

func toWireCacheDiagnostics(d *event.CacheDiagnostics) *wireCacheDiagnostics {
	return &wireCacheDiagnostics{
		PrefixHash:          d.PrefixHash,
		PrefixChanged:       d.PrefixChanged,
		PrefixChangeReasons: append([]string(nil), d.PrefixChangeReasons...),
		SystemHash:          d.SystemHash,
		ToolsHash:           d.ToolsHash,
		LogRewriteVersion:   d.LogRewriteVersion,
		ToolSchemaTokens:    d.ToolSchemaTokens,
		CacheMissTokens:     d.CacheMissTokens,
		CacheHitTokens:      d.CacheHitTokens,
	}
}
