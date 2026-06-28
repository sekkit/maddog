package costwrap

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"maddog/internal/loop"
	"maddog/internal/provider"
)

func TestWrapperTracksOutputBudget(t *testing.T) {
	base := fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{CompletionTokens: 3}},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{CompletionTokens: 4}},
		{Type: provider.ChunkDone},
	}}
	var output atomic.Int64
	wrapped := New(base, &output, 5)

	ch, err := wrapped.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if got := wrapped.OutputTokens(); got != 7 {
		t.Fatalf("OutputTokens = %d, want 7", got)
	}
	if output.Load() != 7 {
		t.Fatalf("external counter = %d, want 7", output.Load())
	}
	if !wrapped.Exceeded() {
		t.Fatal("budget should be exceeded")
	}
	if wrapped.BudgetLimit() != 5 {
		t.Fatalf("BudgetLimit = %d, want 5", wrapped.BudgetLimit())
	}
}

func TestWrapperRejectsRequestsAfterBudgetExceeded(t *testing.T) {
	base := fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{CompletionTokens: 6}},
		{Type: provider.ChunkDone},
	}}
	var output atomic.Int64
	wrapped := New(base, &output, 5)

	ch, err := wrapped.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if _, err := wrapped.Stream(context.Background(), provider.Request{}); !errors.Is(err, loop.ErrBudgetExceeded) {
		t.Fatalf("second Stream err = %v, want ErrBudgetExceeded", err)
	}
}

func TestWrapperAggregatesUsageSnapshot(t *testing.T) {
	base := fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     10,
			CompletionTokens: 4,
			TotalTokens:      14,
			CacheHitTokens:   7,
			CacheMissTokens:  3,
			ReasoningTokens:  1,
		}},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     20,
			CompletionTokens: 6,
			TotalTokens:      26,
			CacheHitTokens:   5,
			CacheMissTokens:  15,
			ReasoningTokens:  2,
		}},
		{Type: provider.ChunkDone},
	}}
	var output atomic.Int64
	wrapped := New(base, &output, 20)

	ch, err := wrapped.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	got := wrapped.UsageSnapshot()
	if got.RequestCount != 2 {
		t.Fatalf("RequestCount = %d, want 2", got.RequestCount)
	}
	if got.PromptTokens != 30 || got.CompletionTokens != 10 || got.TotalTokens != 40 {
		t.Fatalf("usage totals = prompt:%d completion:%d total:%d, want 30/10/40",
			got.PromptTokens, got.CompletionTokens, got.TotalTokens)
	}
	if got.CacheHitTokens != 12 || got.CacheMissTokens != 18 || got.ReasoningTokens != 3 {
		t.Fatalf("usage details = hit:%d miss:%d reasoning:%d, want 12/18/3",
			got.CacheHitTokens, got.CacheMissTokens, got.ReasoningTokens)
	}
	if wrapped.BudgetRemaining() != 10 {
		t.Fatalf("BudgetRemaining = %d, want 10", wrapped.BudgetRemaining())
	}
}

type fakeProvider struct {
	chunks []provider.Chunk
}

func (f fakeProvider) Name() string { return "fake" }

func (f fakeProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, len(f.chunks))
	for _, chunk := range f.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}
