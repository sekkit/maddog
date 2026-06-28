package costwrap

import (
	"context"
	"sync/atomic"

	"reasonix/internal/loop"
	"reasonix/internal/provider"
)

type Wrapper struct {
	base       provider.Provider
	output     *atomic.Int64
	limit      int64
	requests   atomic.Int64
	prompt     atomic.Int64
	completion atomic.Int64
	total      atomic.Int64
	cacheHit   atomic.Int64
	cacheMiss  atomic.Int64
	reasoning  atomic.Int64
}

type UsageSnapshot struct {
	RequestCount     int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheHitTokens   int
	CacheMissTokens  int
	ReasoningTokens  int
}

func New(base provider.Provider, output *atomic.Int64, limit int64) *Wrapper {
	return &Wrapper{base: base, output: output, limit: limit}
}

func (w *Wrapper) Name() string {
	if w == nil || w.base == nil {
		return ""
	}
	return w.base.Name()
}

func (w *Wrapper) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if w != nil && w.Exceeded() {
		return nil, loop.ErrBudgetExceeded
	}
	ch, err := w.base.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		for chunk := range ch {
			if chunk.Type == provider.ChunkUsage && chunk.Usage != nil {
				w.recordUsage(chunk.Usage)
			}
			out <- chunk
		}
	}()
	return out, nil
}

func (w *Wrapper) OutputTokens() int64 {
	if w == nil || w.output == nil {
		return 0
	}
	return w.output.Load()
}

func (w *Wrapper) BudgetLimit() int64 {
	if w == nil {
		return 0
	}
	return w.limit
}

func (w *Wrapper) BudgetRemaining() int64 {
	if w == nil || w.limit <= 0 {
		return 0
	}
	remaining := w.limit - w.OutputTokens()
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (w *Wrapper) UsageSnapshot() UsageSnapshot {
	if w == nil {
		return UsageSnapshot{}
	}
	return UsageSnapshot{
		RequestCount:     int(w.requests.Load()),
		PromptTokens:     int(w.prompt.Load()),
		CompletionTokens: int(w.completion.Load()),
		TotalTokens:      int(w.total.Load()),
		CacheHitTokens:   int(w.cacheHit.Load()),
		CacheMissTokens:  int(w.cacheMiss.Load()),
		ReasoningTokens:  int(w.reasoning.Load()),
	}
}

func (w *Wrapper) Exceeded() bool {
	return w.limit > 0 && w.OutputTokens() >= w.limit
}

func (w *Wrapper) recordUsage(u *provider.Usage) {
	if w == nil || u == nil {
		return
	}
	w.requests.Add(1)
	w.addPositive(&w.prompt, u.PromptTokens)
	w.addPositive(&w.completion, u.CompletionTokens)
	w.addPositive(&w.total, u.TotalTokens)
	w.addPositive(&w.cacheHit, u.CacheHitTokens)
	w.addPositive(&w.cacheMiss, u.CacheMissTokens)
	w.addPositive(&w.reasoning, u.ReasoningTokens)
	if u.CompletionTokens > 0 {
		w.addOutput(int64(u.CompletionTokens))
	}
}

func (w *Wrapper) addPositive(counter *atomic.Int64, n int) {
	if w == nil || counter == nil || n <= 0 {
		return
	}
	counter.Add(int64(n))
}

func (w *Wrapper) addOutput(n int64) {
	if w == nil || w.output == nil || n <= 0 {
		return
	}
	w.output.Add(n)
}
