package costwrap

import (
	"context"
	"sync/atomic"
	"testing"

	"maddog/internal/provider"
)

type fakeProvider struct {
	chunks []provider.Chunk
}

func (p fakeProvider) Name() string { return "fake" }

func (p fakeProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, len(p.chunks))
	for _, chunk := range p.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestCostTrackingProviderAccumulatesOutputTokens(t *testing.T) {
	var output atomic.Int64
	wrapped := New(fakeProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{CompletionTokens: 3}},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{CompletionTokens: 4}},
	}}, &output, 6)
	tracked, ok := wrapped.(*Provider)
	if !ok {
		t.Fatalf("New returned %T, want *Provider", wrapped)
	}

	ch, err := tracked.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if got := tracked.OutputTokens(); got != 7 {
		t.Fatalf("OutputTokens = %d, want 7", got)
	}
	if !tracked.Exceeded() {
		t.Fatal("Exceeded = false, want true after crossing budget")
	}
	if output.Load() != 7 {
		t.Fatalf("shared counter = %d, want 7", output.Load())
	}
}
