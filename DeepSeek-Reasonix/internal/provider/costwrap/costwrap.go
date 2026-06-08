// Package costwrap adds session-scoped output-token accounting around a provider.
package costwrap

import (
	"context"
	"sync/atomic"

	"reasonix/internal/provider"
)

// Provider wraps a chat provider and records frontier output-token usage.
type Provider struct {
	inner       provider.Provider
	output      *atomic.Int64
	budgetLimit int64
}

// New returns inner unchanged when it is nil. When output is nil, New allocates a
// private counter; callers that need to share totals with routing should pass one.
func New(inner provider.Provider, output *atomic.Int64, budgetLimit int64) provider.Provider {
	if inner == nil {
		return nil
	}
	if output == nil {
		output = &atomic.Int64{}
	}
	return &Provider{inner: inner, output: output, budgetLimit: budgetLimit}
}

func (p *Provider) Name() string { return p.inner.Name() }

// Stream forwards chunks while accumulating usage.CompletionTokens. A streaming
// provider can only know final usage after the request, so budget enforcement is
// exposed via Exceeded for the agent to downgrade before the next frontier call.
func (p *Provider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch, err := p.inner.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		for chunk := range ch {
			if chunk.Type == provider.ChunkUsage && chunk.Usage != nil {
				p.output.Add(int64(chunk.Usage.CompletionTokens))
			}
			select {
			case <-ctx.Done():
				out <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
				return
			case out <- chunk:
			}
		}
	}()
	return out, nil
}

func (p *Provider) OutputTokens() int64 {
	if p == nil || p.output == nil {
		return 0
	}
	return p.output.Load()
}

func (p *Provider) BudgetLimit() int64 {
	if p == nil {
		return 0
	}
	return p.budgetLimit
}

func (p *Provider) Exceeded() bool {
	limit := p.BudgetLimit()
	return limit > 0 && p.OutputTokens() >= limit
}

var _ provider.Provider = (*Provider)(nil)
