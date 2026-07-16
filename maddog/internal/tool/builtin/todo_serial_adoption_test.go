package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTodoWriteRejectsNonSerialState(t *testing.T) {
	_, err := (todoWrite{}).Execute(context.Background(), json.RawMessage(`{"todos":[
		{"content":"first","status":"in_progress"},
		{"content":"second","status":"in_progress"}]}`))
	if err == nil || !strings.Contains(err.Error(), "second in_progress") {
		t.Fatalf("two current todos should be rejected, got %v", err)
	}
}

func TestTodoWriteAcceptsActiveSubStepUnderPendingPhase(t *testing.T) {
	_, err := (todoWrite{}).Execute(context.Background(), json.RawMessage(`{"todos":[
		{"content":"Phase","status":"pending"},
		{"content":"sub","status":"in_progress","level":1}]}`))
	if err != nil {
		t.Fatalf("serial phase chain rejected: %v", err)
	}
}
