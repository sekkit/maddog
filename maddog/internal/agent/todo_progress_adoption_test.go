package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"maddog/internal/agent/testutil"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/tool"

	_ "maddog/internal/tool/builtin"
)

func TestTodoProgressGuardPausesRepeatedHostWork(t *testing.T) {
	turns := []testutil.Turn{{ToolCalls: []provider.ToolCall{{
		ID: "todo", Name: "todo_write",
		Arguments: `{"todos":[{"content":"finish","status":"in_progress"}]}`,
	}}}}
	for i := 0; i < maxTodoStallRounds+1; i++ {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("read-%d", i), Name: "inspect", Arguments: `{"path":"same"}`,
		}}})
	}

	registry := tool.NewRegistry()
	registry.Add(fakeTool{name: "inspect", readOnly: true})
	builtin, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin missing")
	}
	registry.Add(builtin)
	agent := New(testutil.NewMock("m", turns...), registry, NewSession(""), Options{}, event.Discard)

	err := agent.Run(context.Background(), "finish the todo")
	var pause *todoStallPause
	if !errors.As(err, &pause) {
		t.Fatalf("Run error = %v, want todoStallPause", err)
	}
	if !sessionContains(agent, "Host progress check") {
		t.Fatal("adaptive progress nudge was not added after eight stalled rounds")
	}
}

func TestCanonicalTodoProgressIgnoresPendingListChurn(t *testing.T) {
	agent := &Agent{todoState: []evidence.TodoItem{
		{Content: "finish", Status: "in_progress"},
		{Content: "test", Status: "pending"},
	}}
	before, tracking := agent.canonicalTodoProgress()
	if !tracking {
		t.Fatal("incomplete todo list should be tracked")
	}
	agent.setTodoState([]evidence.TodoItem{
		{Content: "finish carefully", Status: "in_progress"},
		{Content: "test", Status: "pending"},
		{Content: "document", Status: "pending"},
	})
	after, tracking := agent.canonicalTodoProgress()
	if !tracking || after != before {
		t.Fatalf("pending/title churn changed semantic progress from %d to %d", before, after)
	}
}
