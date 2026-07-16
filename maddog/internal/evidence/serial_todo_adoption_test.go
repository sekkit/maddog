package evidence

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSerialTodosRequiresOneCurrentItem(t *testing.T) {
	tests := []struct {
		name  string
		todos []TodoItem
		want  string
	}{
		{
			name: "two current items",
			todos: []TodoItem{
				{Content: "first", Status: "in_progress"},
				{Content: "second", Status: "in_progress"},
			},
			want: "second in_progress",
		},
		{
			name: "pending work without a current item",
			todos: []TodoItem{
				{Content: "first", Status: "completed"},
				{Content: "second", Status: "pending"},
			},
			want: "no in_progress",
		},
		{
			name: "completed work after current item",
			todos: []TodoItem{
				{Content: "first", Status: "in_progress"},
				{Content: "second", Status: "completed"},
			},
			want: "completed after unfinished",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSerialTodos(tc.todos); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateSerialTodos() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAdvanceSerialTodoWalksPhaseChain(t *testing.T) {
	todos := []TodoItem{
		{Content: "Phase", Status: "pending"},
		{Content: "sub one", Status: "in_progress", Level: 1},
		{Content: "sub two", Status: "pending", Level: 1},
		{Content: "Next", Status: "pending"},
	}
	if !AdvanceSerialTodo(todos, 1) || !reflect.DeepEqual(statuses(todos), []string{"pending", "completed", "in_progress", "pending"}) {
		t.Fatalf("first sub-step did not advance serially: %+v", todos)
	}
	if !AdvanceSerialTodo(todos, 2) || !reflect.DeepEqual(statuses(todos), []string{"in_progress", "completed", "completed", "pending"}) {
		t.Fatalf("last sub-step did not return phase for sign-off: %+v", todos)
	}
	if !AdvanceSerialTodo(todos, 0) || !reflect.DeepEqual(statuses(todos), []string{"completed", "completed", "completed", "in_progress"}) {
		t.Fatalf("phase sign-off did not promote next item: %+v", todos)
	}
}

func TestSuccessfulProgressSignaturesKeepIdentityStable(t *testing.T) {
	ledger := NewLedger()
	read := ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"a.go"}`), true, true)
	read.OutputBytes = 10
	ledger.Record(read)
	ledger.Record(read)
	ledger.Record(ReceiptFromToolCall("todo_write", json.RawMessage(`{"todos":[]}`), true, true))
	ledger.Record(ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"a.go","old_string":"a","new_string":"b"}`), true, false))
	ledger.Record(ReceiptFromToolCall("bash", json.RawMessage(`{"command":"go test ./..."}`), true, false))

	sigs := ledger.SuccessfulProgressSignaturesSince(0)
	if len(sigs) != 4 {
		t.Fatalf("progress signatures = %d, want repeated reads plus mutation and command", len(sigs))
	}
	if sigs[0] != sigs[1] {
		t.Fatalf("exact repeated reads should share identity: %q != %q", sigs[0], sigs[1])
	}
	if sigs[1] == sigs[2] || sigs[2] == sigs[3] {
		t.Fatalf("distinct work collapsed to one signature: %v", sigs)
	}
}

func statuses(todos []TodoItem) []string {
	out := make([]string, len(todos))
	for i, todo := range todos {
		out[i] = todo.Status
	}
	return out
}
