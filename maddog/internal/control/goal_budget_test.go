package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type goalBudgetCountingRunner struct {
	calls int
}

func (r *goalBudgetCountingRunner) Run(context.Context, string) error {
	r.calls++
	return nil
}

type goalBudgetErrorRunner struct {
	err     error
	started chan struct{}
}

func (r *goalBudgetErrorRunner) Run(ctx context.Context, _ string) error {
	if r.started != nil {
		close(r.started)
	}
	if r.err != nil {
		return r.err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestGoalBudgetUsagePersistsAcrossResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	c := newGoalPersistenceController(path)
	c.executor.SeedTodoState([]evidence.TodoItem{{Content: "finish verification", Status: "in_progress"}})
	c.SetGoalWithOptions("ship within budget", GoalOptions{
		ResearchMode:      GoalResearchOn,
		TurnBudget:        7,
		TokenBudget:       10,
		TimeBudgetSeconds: 90,
		Strict:            true,
	})
	started := c.GoalSnapshot()
	if started.TurnBudget != 7 || started.TokenBudget != 10 || started.TimeBudgetSeconds != 90 || !started.Strict {
		t.Fatalf("started budget = %+v", started)
	}

	// Providers that omit TotalTokens fall back to prompt + completion.
	c.recordGoalUsage(provider.Usage{PromptTokens: 3, CompletionTokens: 2})
	c.recordGoalUsage(provider.Usage{PromptTokens: 100, CompletionTokens: 100, TotalTokens: 5})
	limited := c.GoalSnapshot()
	if limited.TokensUsed != 10 || limited.Status != GoalStatusBudgetLimited {
		t.Fatalf("limited budget = %+v", limited)
	}
	if limited.ID != started.ID || limited.Objective != started.Objective || limited.TerminalAt.IsZero() {
		t.Fatalf("budget terminal identity = %+v, started %+v", limited, started)
	}

	resumed := newGoalPersistenceController(filepath.Join(t.TempDir(), "placeholder.jsonl"))
	resumed.Resume(agent.NewSession("sys"), path)
	got := resumed.GoalSnapshot()
	if got.Status != GoalStatusBudgetLimited || got.TokensUsed != 10 || got.TokenBudget != 10 || got.TurnBudget != 7 || got.TimeBudgetSeconds != 90 {
		t.Fatalf("resumed budget = %+v", got)
	}
	if got.ID != started.ID || got.Objective != started.Objective {
		t.Fatalf("resumed identity = %+v, started %+v", got, started)
	}
	if todos := resumed.Todos(); len(todos) != 1 || todos[0].Content != "finish verification" {
		t.Fatalf("resumed budget-limited todos = %+v", todos)
	}

	defaults := New(Options{})
	defaults.SetGoal("use defaults")
	if snapshot := defaults.GoalSnapshot(); snapshot.TurnBudget != defaultGoalTurnBudget || snapshot.TokenBudget != 0 || snapshot.TimeBudgetSeconds != 0 {
		t.Fatalf("default budget = %+v", snapshot)
	}
}

func TestGoalBudgetGatePreventsProviderTurn(t *testing.T) {
	newController := func(runner *goalBudgetCountingRunner) *Controller {
		exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
		return New(Options{Runner: runner, Executor: exec, Sink: event.Discard})
	}

	t.Run("turn budget", func(t *testing.T) {
		runner := &goalBudgetCountingRunner{}
		c := newController(runner)
		c.SetGoalWithOptions("one turn only", GoalOptions{TurnBudget: 1})
		snapshot := c.GoalSnapshot()
		res := c.goals.advance(goalAdvanceInput{generation: snapshot.Generation, status: GoalStatusRunning, toolCalled: true})
		c.persistGoalState(res.path, res.data, res.ok)
		if c.GoalStatus() != GoalStatusBudgetLimited {
			t.Fatalf("status = %q, want budget_limited", c.GoalStatus())
		}
		if err := newTurnOrchestrator(c).runHeadlessTurn(context.Background(), "next", "next", true); err != nil {
			t.Fatal(err)
		}
		if runner.calls != 0 {
			t.Fatalf("provider calls = %d, want 0", runner.calls)
		}
	})

	t.Run("time budget", func(t *testing.T) {
		runner := &goalBudgetCountingRunner{}
		c := newController(runner)
		c.SetGoalWithOptions("finish quickly", GoalOptions{TimeBudgetSeconds: 1})
		c.goals.mu.Lock()
		c.goals.startedAt = time.Now().UTC().Add(-2 * time.Second)
		c.goals.mu.Unlock()
		if err := newTurnOrchestrator(c).runHeadlessTurn(context.Background(), "next", "next", true); err != nil {
			t.Fatal(err)
		}
		if runner.calls != 0 {
			t.Fatalf("provider calls = %d, want 0", runner.calls)
		}
		if snapshot := c.GoalSnapshot(); snapshot.Status != GoalStatusBudgetLimited || snapshot.TimeUsedSeconds < 1 {
			t.Fatalf("time-limited snapshot = %+v", snapshot)
		}
	})
}

func TestGoalCompletionOnLastAllowedTurnWinsOverTurnBudget(t *testing.T) {
	var g goalMachine
	g.setWithOptions("finish on the last turn", GoalOptions{TurnBudget: 1}, nil)
	snapshot := g.durableSnapshot()
	result := g.advance(goalAdvanceInput{
		generation: snapshot.Generation,
		status:     GoalStatusComplete,
		toolCalled: true,
	})
	if result.notice != goalCompleteNotice || g.durableSnapshot().Status != GoalStatusComplete {
		t.Fatalf("last-turn completion = result=%+v snapshot=%+v", result, g.durableSnapshot())
	}
}

func TestGoalCompletionOnTokenBudgetCrossingRoundWinsOverBudget(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "inspect-1", Name: "inspect", Arguments: `{}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 2}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Finished.\n\n[goal:complete]"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 4}},
			{Type: provider.ChunkDone},
		},
	}}
	registry := tool.NewRegistry()
	registry.Add(outputControlTool{name: "inspect", output: "ok"})
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard})
	c.SetGoalWithOptions("inspect then finish at the budget boundary", GoalOptions{TokenBudget: 5})

	if err := c.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want tool round + final round", prov.call)
	}
	snapshot := c.GoalSnapshot()
	if snapshot.Status != GoalStatusComplete || snapshot.TokensUsed != 6 {
		t.Fatalf("completion at token boundary = %+v", snapshot)
	}
}

func TestGoalCreatedMidTurnStructuredCompletionWinsOverTokenBudget(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "goal-create", Name: "goal_control", Arguments: `{"action":"create","objective":"finish in this turn","token_budget":5}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 1}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "goal-complete", Name: "goal_control", Arguments: `{"action":"update","status":"complete"}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 5}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Finished."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 1}},
			{Type: provider.ChunkDone},
		},
	}}
	registry := tool.NewRegistry()
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard})

	if err := c.Run(context.Background(), "create and finish a budgeted goal"); err != nil {
		t.Fatal(err)
	}
	snapshot := c.GoalSnapshot()
	if snapshot.Status != GoalStatusComplete || snapshot.TokensUsed != 6 {
		t.Fatalf("mid-turn structured completion = %+v, want complete with post-create usage", snapshot)
	}
}

func TestGoalCreatedMidTurnMarkerCompletionWinsOverTokenBudget(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "goal-create", Name: "goal_control", Arguments: `{"action":"create","objective":"finish in this turn","token_budget":5}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 1}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Finished.\n\n[goal:complete]"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 5}},
			{Type: provider.ChunkDone},
		},
	}}
	registry := tool.NewRegistry()
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard})

	if err := c.Run(context.Background(), "create and finish a budgeted goal"); err != nil {
		t.Fatal(err)
	}
	snapshot := c.GoalSnapshot()
	if prov.call != 2 || snapshot.Status != GoalStatusComplete || snapshot.TokensUsed != 5 {
		t.Fatalf("mid-turn marker completion calls=%d snapshot=%+v", prov.call, snapshot)
	}
}

func TestControllerUsageObserverCountsEachExecutorProviderRound(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "inspect-1", Name: "inspect", Arguments: `{}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Finished.\n\n[goal:complete]"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}},
			{Type: provider.ChunkDone},
		},
	}}
	registry := tool.NewRegistry()
	registry.Add(outputControlTool{name: "inspect", output: "ok"})
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Sink: event.Discard})
	c.SetGoalWithOptions("inspect then finish", GoalOptions{TokenBudget: 100})

	if err := c.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2", prov.call)
	}
	if snapshot := c.GoalSnapshot(); snapshot.TokensUsed != 10 || snapshot.Status != GoalStatusComplete {
		t.Fatalf("observed usage snapshot = %+v", snapshot)
	}
}

func TestGoalBudgetExcludesPlannerProviderUsage(t *testing.T) {
	planner := &scriptedTurns{turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "Inspect the target, then finish it."},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 80, CompletionTokens: 20, TotalTokens: 100}},
		{Type: provider.ChunkDone},
	}}}
	executorProvider := &scriptedTurns{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "inspect-1", Name: "inspect", Arguments: `{}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Finished.\n\n[goal:complete]"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4}},
			{Type: provider.ChunkDone},
		},
	}}
	registry := tool.NewRegistry()
	registry.Add(outputControlTool{name: "inspect", output: "ok"})
	exec := agent.New(executorProvider, registry, agent.NewSession("executor"), agent.Options{}, event.Discard)
	coord := agent.NewCoordinator(
		planner,
		agent.NewSession("planner"),
		nil,
		nil,
		agent.Options{},
		exec,
		0,
		event.Discard,
		nil,
	)
	c := New(Options{Runner: coord, Executor: exec, Sink: event.Discard})
	c.SetGoalWithOptions("finish coordinated work", GoalOptions{TokenBudget: 50})

	if err := c.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if snapshot := c.GoalSnapshot(); snapshot.TokensUsed != 7 || snapshot.Status != GoalStatusComplete {
		t.Fatalf("coordinated usage snapshot = %+v, want executor-only 7 tokens", snapshot)
	}
}

func TestGoalInterruptionLifecycle(t *testing.T) {
	t.Run("ambient context cancellation remains resumable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		runner := &goalBudgetErrorRunner{}
		exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Runner: runner, Executor: exec, SessionPath: path, SessionDir: filepath.Dir(path)})
		c.SetGoal("resume after cancellation")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.RunTurn(ctx, "start"); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunTurn error = %v, want context.Canceled", err)
		}
		snapshot := c.GoalSnapshot()
		if snapshot.Status != GoalStatusRunning || snapshot.InterruptedAt.IsZero() || !strings.Contains(snapshot.LastError, "canceled") || !snapshot.TerminalAt.IsZero() {
			t.Fatalf("ambient interruption snapshot = %+v", snapshot)
		}
		resumed := newGoalPersistenceController(filepath.Join(t.TempDir(), "placeholder.jsonl"))
		resumed.Resume(agent.NewSession("sys"), path)
		if got := resumed.GoalSnapshot(); got.Status != GoalStatusRunning || got.LastError != snapshot.LastError || got.InterruptedAt.IsZero() {
			t.Fatalf("resumed interruption = %+v", got)
		}
	})

	t.Run("provider error remains resumable", func(t *testing.T) {
		providerErr := errors.New("provider unavailable")
		runner := &goalBudgetErrorRunner{err: providerErr}
		exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Runner: runner, Executor: exec})
		c.SetGoal("retry provider")
		if err := c.RunTurn(context.Background(), "start"); !errors.Is(err, providerErr) {
			t.Fatalf("RunTurn error = %v, want provider error", err)
		}
		if snapshot := c.GoalSnapshot(); snapshot.Status != GoalStatusRunning || snapshot.LastError != providerErr.Error() || snapshot.InterruptedAt.IsZero() {
			t.Fatalf("provider interruption snapshot = %+v", snapshot)
		}
	})

	t.Run("explicit cancel stops goal", func(t *testing.T) {
		started := make(chan struct{})
		runner := &goalBudgetErrorRunner{started: started}
		exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Runner: runner, Executor: exec})
		c.SetGoal("stop on user cancel")
		done := make(chan error, 1)
		go func() { done <- c.RunTurn(context.Background(), "start") }()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("runner did not start")
		}
		c.Cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("RunTurn error = %v, want context.Canceled", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("RunTurn did not stop")
		}
		if snapshot := c.GoalSnapshot(); snapshot.Status != GoalStatusStopped || snapshot.TerminalAt.IsZero() {
			t.Fatalf("user-cancel snapshot = %+v", snapshot)
		}
	})
}
