package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maddog/internal/agent"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type goalControlProgressTool struct{}

func (goalControlProgressTool) Name() string            { return "goal_control_progress" }
func (goalControlProgressTool) Description() string     { return "records test progress" }
func (goalControlProgressTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (goalControlProgressTool) ReadOnly() bool          { return false }
func (goalControlProgressTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "progress recorded", nil
}

func newGoalControlToolTest(t *testing.T) (*goalControlTool, *Controller, *agent.Agent) {
	t.Helper()
	executor := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, Sink: event.Discard})
	return newGoalControlTool(controller), controller, executor
}

func executeGoalControl(t *testing.T, tool *goalControlTool, args string) (GoalSnapshot, error) {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		return GoalSnapshot{}, err
	}
	var snapshot GoalSnapshot
	if err := json.Unmarshal([]byte(result), &snapshot); err != nil {
		t.Fatalf("decode goal_control result %q: %v", result, err)
	}
	return snapshot, nil
}

func TestGoalControlToolContract(t *testing.T) {
	goalTool, _, _ := newGoalControlToolTest(t)
	if goalTool.Name() != "goal_control" {
		t.Fatalf("Name() = %q", goalTool.Name())
	}
	if goalTool.ReadOnly() {
		t.Fatal("goal_control must not be read-only")
	}
	if goalTool.PlanModeSafe() {
		t.Fatal("goal_control must not be plan-mode safe")
	}

	var schema struct {
		AdditionalProperties bool `json:"additionalProperties"`
		Properties           map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(goalTool.Schema(), &schema); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	if schema.AdditionalProperties {
		t.Fatal("schema must set additionalProperties=false")
	}
	if got := strings.Join(schema.Properties["action"].Enum, ","); got != "get,create,update,clear" {
		t.Fatalf("action enum = %q", got)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "action" {
		t.Fatalf("required = %v", schema.Required)
	}
}

func TestGoalControlGetAndCreate(t *testing.T) {
	goalTool, controller, _ := newGoalControlToolTest(t)
	empty, err := executeGoalControl(t, goalTool, `{"action":"get"}`)
	if err != nil {
		t.Fatal(err)
	}
	if empty.SchemaVersion != GoalSnapshotSchemaVersion || empty.Status != "" || empty.Objective != "" {
		t.Fatalf("empty snapshot = %+v", empty)
	}

	created, err := executeGoalControl(t, goalTool, `{
		"action":"create",
		"objective":"  ship structured goals  ",
		"strict":true,
		"research_mode":"on",
		"turn_budget":12,
		"token_budget":3456,
		"time_budget_seconds":78
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if created.Objective != "ship structured goals" || created.Goal != created.Objective || created.Status != GoalStatusRunning {
		t.Fatalf("created identity = %+v", created)
	}
	if !created.Strict || created.ResearchMode != GoalResearchOn || created.TurnBudget != 12 || created.TokenBudget != 3456 || created.TimeBudgetSeconds != 78 {
		t.Fatalf("created options = %+v", created)
	}
	if created.ID == "" || created.Generation == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("created durable fields = %+v", created)
	}
	if got := controller.GoalSnapshot(); got.ID != created.ID || got.Revision != created.Revision {
		t.Fatalf("controller snapshot = %+v, tool result = %+v", got, created)
	}

	if _, err := executeGoalControl(t, goalTool, `{"action":"create","objective":"replacement"}`); err == nil || !strings.Contains(err.Error(), "while goal") {
		t.Fatalf("create while running error = %v", err)
	}
	if got := controller.GoalSnapshot(); got.ID != created.ID || got.Objective != created.Objective {
		t.Fatalf("rejected create mutated state: %+v", got)
	}
}

func TestGoalControlUpdateCompleteUsesExecutorReadinessAndTodos(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Add(goalControlProgressTool{})
	provider := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("progress-1", "goal_control_progress", `{}`),
		textTurn("work finished"),
	}}
	executor := agent.New(provider, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	if err := executor.Run(context.Background(), "do real work"); err != nil {
		t.Fatalf("seed execution evidence: %v", err)
	}
	controller := New(Options{Runner: executor, Executor: executor, Sink: event.Discard})
	goalTool := newGoalControlTool(controller)
	executor.ReplaceTodoState([]evidence.TodoItem{{Content: "run tests", Status: "in_progress"}})
	if _, err := executeGoalControl(t, goalTool, `{"action":"create","objective":"finish safely"}`); err != nil {
		t.Fatal(err)
	}

	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"complete"}`); err == nil || !strings.Contains(err.Error(), "run tests") {
		t.Fatalf("readiness rejection = %v", err)
	}
	if got := controller.GoalSnapshot(); got.Status != GoalStatusRunning || got.Goal == "" {
		t.Fatalf("readiness rejection mutated state: %+v", got)
	}
	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"complete","todos":[]}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("model-supplied todos error = %v", err)
	}

	executor.ReplaceTodoState([]evidence.TodoItem{{Content: "run tests", Status: "completed"}})
	completed, err := executeGoalControl(t, goalTool, `{"action":"update","status":"complete"}`)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != GoalStatusComplete || completed.Goal != "" || completed.Objective != "finish safely" || completed.TerminalAt.IsZero() {
		t.Fatalf("completed snapshot = %+v", completed)
	}
	if len(completed.Todos) != 1 || completed.Todos[0].Content != "run tests" || completed.Todos[0].Status != "completed" {
		t.Fatalf("completed todos = %+v", completed.Todos)
	}
	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"complete"}`); err == nil || !strings.Contains(err.Error(), "complete status") {
		t.Fatalf("second completion error = %v", err)
	}
}

func TestGoalControlUpdateCompleteRejectsCanonicalTodoWithoutReceipt(t *testing.T) {
	goalTool, controller, executor := newGoalControlToolTest(t)
	executor.SeedTodoState([]evidence.TodoItem{{Content: "finish host-seeded work", Status: "in_progress"}})
	if _, err := executeGoalControl(t, goalTool, `{"action":"create","objective":"finish safely"}`); err != nil {
		t.Fatal(err)
	}

	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"complete"}`); err == nil || !strings.Contains(err.Error(), "finish host-seeded work") {
		t.Fatalf("canonical todo readiness error = %v", err)
	}
	if got := controller.GoalSnapshot(); got.Status != GoalStatusRunning || got.Goal == "" {
		t.Fatalf("rejected completion mutated state: %+v", got)
	}
}

func TestGoalControlUpdateBlockedAndClear(t *testing.T) {
	goalTool, _, _ := newGoalControlToolTest(t)
	if _, err := executeGoalControl(t, goalTool, `{"action":"create","objective":"deploy release"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"blocked"}`); err == nil || !strings.Contains(err.Error(), "requires a non-empty reason") {
		t.Fatalf("missing block reason error = %v", err)
	}

	blockedOnce, err := executeGoalControl(t, goalTool, `{"action":"update","status":"blocked","reason":"  production credentials required  "}`)
	if err != nil {
		t.Fatal(err)
	}
	if blockedOnce.Status != GoalStatusRunning || blockedOnce.Block != "production credentials required" || blockedOnce.Blocks != 1 || !blockedOnce.TerminalAt.IsZero() {
		t.Fatalf("first blocked audit = %+v", blockedOnce)
	}
	blockedTwice, err := executeGoalControl(t, goalTool, `{"action":"update","status":"blocked","reason":"PRODUCTION-CREDENTIALS REQUIRED!"}`)
	if err != nil {
		t.Fatal(err)
	}
	if blockedTwice.Status != GoalStatusRunning || blockedTwice.Blocks != 2 || !blockedTwice.TerminalAt.IsZero() {
		t.Fatalf("second blocked audit = %+v", blockedTwice)
	}
	blocked, err := executeGoalControl(t, goalTool, `{"action":"update","status":"blocked","reason":"production credentials required"}`)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != GoalStatusBlocked || blocked.Blocks != 3 || blocked.TerminalAt.IsZero() {
		t.Fatalf("third blocked audit = %+v", blocked)
	}
	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"blocked","reason":"still blocked"}`); err == nil || !strings.Contains(err.Error(), "blocked status") {
		t.Fatalf("post-terminal blocked update error = %v", err)
	}

	cleared, err := executeGoalControl(t, goalTool, `{"action":"clear"}`)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Status != GoalStatusStopped || cleared.Goal != "" || cleared.Objective != "deploy release" {
		t.Fatalf("cleared snapshot = %+v", cleared)
	}
	if _, err := executeGoalControl(t, goalTool, `{"action":"create","objective":"next goal"}`); err != nil {
		t.Fatalf("create after clear: %v", err)
	}
}

func TestGoalControlBlockedAuditAdvancesOncePerRunnerUnit(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("blocked-1", "goal_control", `{"action":"update","status":"blocked","reason":"needs credentials"}`),
		textTurn("Recorded the blocker."),
		toolCallTurn("blocked-2", "goal_control", `{"action":"update","status":"blocked","reason":"NEEDS-CREDENTIALS!"}`),
		textTurn("The blocker remains."),
		toolCallTurn("blocked-3", "goal_control", `{"action":"update","status":"blocked","reason":"needs credentials"}`),
		textTurn("Still blocked."),
	}}
	registry := tool.NewRegistry()
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard})
	c.SetGoal("deploy the release")

	if err := c.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	snapshot := c.GoalSnapshot()
	if prov.call != 6 || snapshot.Status != GoalStatusBlocked || snapshot.Blocks != 3 || snapshot.Turns != 3 {
		t.Fatalf("structured blocked audit calls=%d snapshot=%+v", prov.call, snapshot)
	}
}

func TestGoalControlBlockedAuditCountsAtMostOncePerRunnerUnit(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "blocked-a", Name: "goal_control", Arguments: `{"action":"update","status":"blocked","reason":"needs credentials"}`}},
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "blocked-b", Name: "goal_control", Arguments: `{"action":"update","status":"blocked","reason":"needs credentials"}`}},
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "blocked-c", Name: "goal_control", Arguments: `{"action":"update","status":"blocked","reason":"needs credentials"}`}},
			{Type: provider.ChunkDone},
		},
		textTurn("Recorded one blocker report."),
		textTurn("Recovered after another goal turn.\n\n[goal:complete]"),
	}}
	registry := tool.NewRegistry()
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard})
	c.SetGoal("deploy the release")

	if err := c.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	snapshot := c.GoalSnapshot()
	if prov.call != 3 || snapshot.Status != GoalStatusComplete || snapshot.Turns != 2 {
		t.Fatalf("same-unit blocker reports calls=%d snapshot=%+v", prov.call, snapshot)
	}
}

func TestGoalControlStrictCompleteRunsSelfCheckBeforeTerminal(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("complete-1", "goal_control", `{"action":"update","status":"complete"}`),
		textTurn("Self-check requested."),
		toolCallTurn("complete-2", "goal_control", `{"action":"update","status":"complete"}`),
		textTurn("Self-check passed."),
	}}
	registry := tool.NewRegistry()
	exec := agent.New(prov, registry, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard})
	c.SetGoalWithOptions("verify the release", GoalOptions{Strict: true})

	if err := c.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	snapshot := c.GoalSnapshot()
	if prov.call != 4 || snapshot.Status != GoalStatusComplete || snapshot.Turns != 2 || snapshot.SelfCheckDone {
		t.Fatalf("strict structured completion calls=%d snapshot=%+v", prov.call, snapshot)
	}
}

func TestGoalControlRejectsInvalidActionsAndArguments(t *testing.T) {
	goalTool, _, _ := newGoalControlToolTest(t)
	tests := []struct {
		name string
		args string
		want string
	}{
		{name: "missing action", args: `{}`, want: "requires an action"},
		{name: "unknown action", args: `{"action":"pause"}`, want: "action \"pause\" is invalid"},
		{name: "unknown field", args: `{"action":"get","extra":true}`, want: "unknown field"},
		{name: "trailing value", args: `{"action":"get"} {}`, want: "multiple JSON values"},
		{name: "get with create field", args: `{"action":"get","strict":false}`, want: "get accepts only"},
		{name: "clear with update field", args: `{"action":"clear","status":"blocked"}`, want: "clear accepts only"},
		{name: "empty objective", args: `{"action":"create","objective":"  "}`, want: "non-empty objective"},
		{name: "bad research mode", args: `{"action":"create","objective":"x","research_mode":"sometimes"}`, want: "research_mode"},
		{name: "zero turn budget", args: `{"action":"create","objective":"x","turn_budget":0}`, want: "turn_budget"},
		{name: "negative token budget", args: `{"action":"create","objective":"x","token_budget":-1}`, want: "token_budget"},
		{name: "negative time budget", args: `{"action":"create","objective":"x","time_budget_seconds":-1}`, want: "time_budget_seconds"},
		{name: "create transition fields", args: `{"action":"create","objective":"x","status":"complete"}`, want: "does not accept status"},
		{name: "update create fields", args: `{"action":"update","status":"complete","strict":true}`, want: "accepts only status"},
		{name: "missing update status", args: `{"action":"update"}`, want: "requires status"},
		{name: "invalid update status", args: `{"action":"update","status":"running"}`, want: "status \"running\" is invalid"},
		{name: "complete with reason", args: `{"action":"update","status":"complete","reason":"done"}`, want: "does not accept a reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := executeGoalControl(t, goalTool, test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestGoalControlUpdateRequiresRunningGoalAndExecutor(t *testing.T) {
	goalTool, _, _ := newGoalControlToolTest(t)
	if _, err := executeGoalControl(t, goalTool, `{"action":"update","status":"blocked","reason":"nothing started"}`); err == nil || !strings.Contains(err.Error(), "stopped status") {
		t.Fatalf("update without goal error = %v", err)
	}

	controller := New(Options{Sink: event.Discard})
	controller.SetGoal("executor-less goal")
	withoutExecutor := newGoalControlTool(controller)
	if _, err := executeGoalControl(t, withoutExecutor, `{"action":"update","status":"complete"}`); err == nil || !strings.Contains(err.Error(), "without an executor") {
		t.Fatalf("complete without executor error = %v", err)
	}
}
