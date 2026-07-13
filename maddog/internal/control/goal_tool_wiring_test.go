package control

import (
	"testing"

	"maddog/internal/tool"
)

func TestControllerRegistersGoalControlTool(t *testing.T) {
	registry := tool.NewRegistry()
	c := New(Options{Registry: registry})

	got, ok := c.ToolRegistry().Get("goal_control")
	if !ok {
		t.Fatal("goal_control is not registered in the controller tool registry")
	}
	if got.Name() != "goal_control" {
		t.Fatalf("registered goal tool name = %q, want goal_control", got.Name())
	}
}
