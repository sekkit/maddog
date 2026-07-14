package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"maddog/internal/agent/testutil"
	"maddog/internal/event"
	"maddog/internal/provider"
)

func TestUsageObserverReceivesOneCopyPerCompletedProviderRound(t *testing.T) {
	toolUsage := &provider.Usage{
		PromptTokens:     11,
		CompletionTokens: 3,
		TotalTokens:      14,
		Iterations:       []provider.UsageIteration{{Model: "tool-model", InputTokens: 11, OutputTokens: 3}},
	}
	finalUsage := &provider.Usage{PromptTokens: 17, CompletionTokens: 5, TotalTokens: 22}
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}, Usage: toolUsage},
		testutil.Turn{Text: "done", Usage: finalUsage},
	)

	var mu sync.Mutex
	var observed []provider.Usage
	var usageEvents int
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage {
			usageEvents++
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{MaxSteps: 2}, sink)
	a.SetUsageObserver(func(u provider.Usage) {
		mu.Lock()
		observed = append(observed, u)
		mu.Unlock()
	})

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	got := append([]provider.Usage(nil), observed...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("observer calls = %d, want one for each completed provider round", len(got))
	}
	if usageEvents != 2 {
		t.Fatalf("usage events = %d, want one for each valid usage", usageEvents)
	}
	if got[0].TotalTokens != 14 || got[1].TotalTokens != 22 {
		t.Fatalf("observed totals = [%d %d], want [14 22]", got[0].TotalTokens, got[1].TotalTokens)
	}

	// The observer receives an independent value, including nested iteration data.
	toolUsage.TotalTokens = 99
	toolUsage.Iterations[0].OutputTokens = 99
	if got[0].TotalTokens != 14 || got[0].Iterations[0].OutputTokens != 3 {
		t.Fatalf("observer value was aliased to provider usage: %+v", got[0])
	}
}

func TestUsageObserverIgnoresEmptyUsage(t *testing.T) {
	mp := testutil.NewMock("m", testutil.Turn{Text: "done", Usage: &provider.Usage{}})
	var calls, usageEvents int
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage {
			usageEvents++
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)
	a.SetUsageObserver(func(provider.Usage) { calls++ })

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 0 || usageEvents != 0 {
		t.Fatalf("empty usage produced observer calls=%d events=%d, want both zero", calls, usageEvents)
	}
}

func TestUsageObserverDoesNotCountInterruptedStreamRecovery(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("unexpected EOF")}
	interruptedUsage := &provider.Usage{PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9}
	recoveredUsage := &provider.Usage{PromptTokens: 13, CompletionTokens: 4, TotalTokens: 17}
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "partial", Usage: interruptedUsage, ChunkError: interrupted},
		testutil.Turn{Text: "recovered", Usage: recoveredUsage},
	)
	var observed []provider.Usage
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	a.SetUsageObserver(func(u provider.Usage) { observed = append(observed, u) })

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover interrupted stream: %v", err)
	}
	if len(observed) != 1 {
		t.Fatalf("observer calls = %d, want one successful recovery round", len(observed))
	}
	if observed[0].TotalTokens != 17 {
		t.Fatalf("observed recovered usage = %+v, want total 17", observed[0])
	}
}

func TestUsageObserverCanBeClearedAndPanicsAreContained(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "first", Usage: &provider.Usage{TotalTokens: 3}},
		testutil.Turn{Text: "second", Usage: &provider.Usage{TotalTokens: 4}},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	var calls int
	a.SetUsageObserver(func(provider.Usage) {
		calls++
		panic("observer failure")
	})

	if err := a.Run(context.Background(), "first"); err != nil {
		t.Fatalf("Run with panicking observer: %v", err)
	}
	if calls != 1 {
		t.Fatalf("observer calls = %d, want 1", calls)
	}
	a.SetUsageObserver(nil)
	if err := a.Run(context.Background(), "second"); err != nil {
		t.Fatalf("Run after clearing observer: %v", err)
	}
	if calls != 1 {
		t.Fatalf("observer calls after clear = %d, want 1", calls)
	}
}
