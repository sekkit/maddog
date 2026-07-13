package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"maddog/internal/evidence"
	"maddog/internal/tool"
)

// goalControlTool gives the model an explicit, structured goal-state API. The
// text markers remain available as a compatibility fallback for providers that
// do not call tools reliably.
type goalControlTool struct {
	controller *Controller
}

var _ tool.Tool = (*goalControlTool)(nil)
var _ tool.PlanModeClassifier = (*goalControlTool)(nil)

func newGoalControlTool(controller *Controller) *goalControlTool {
	return &goalControlTool{controller: controller}
}

func (*goalControlTool) Name() string { return "goal_control" }

func (*goalControlTool) Description() string {
	return "Inspect and explicitly manage the current autonomous goal. Use get to read its durable state, create to start an explicitly requested goal when none is running, update complete only after the host's readiness checks pass, and update blocked only after the same blocker is reported on three consecutive goal turns. Clear only when the user explicitly asks to stop the goal. Todo state is always read from the host and cannot be supplied here."
}

func (*goalControlTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"action":{"type":"string","enum":["get","create","update","clear"]},
			"objective":{"type":"string","minLength":1},
			"strict":{"type":"boolean"},
			"research_mode":{"type":"string","enum":["auto","on","off"]},
			"turn_budget":{"type":"integer","minimum":1},
			"token_budget":{"type":"integer","minimum":0},
			"time_budget_seconds":{"type":"integer","minimum":0},
			"status":{"type":"string","enum":["complete","blocked"]},
			"reason":{"type":"string","minLength":1}
		},
		"required":["action"]
	}`)
}

func (*goalControlTool) ReadOnly() bool     { return false }
func (*goalControlTool) PlanModeSafe() bool { return false }

type goalControlArgs struct {
	Action            string  `json:"action"`
	Objective         *string `json:"objective"`
	Strict            *bool   `json:"strict"`
	ResearchMode      *string `json:"research_mode"`
	TurnBudget        *int    `json:"turn_budget"`
	TokenBudget       *int64  `json:"token_budget"`
	TimeBudgetSeconds *int64  `json:"time_budget_seconds"`
	Status            *string `json:"status"`
	Reason            *string `json:"reason"`
}

func (t *goalControlTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.controller == nil {
		return "", fmt.Errorf("goal_control is not attached to a controller")
	}

	args, err := decodeGoalControlArgs(raw)
	if err != nil {
		return "", err
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "get":
		if args.hasStateArguments() {
			return "", fmt.Errorf("goal_control get accepts only the action argument")
		}
		return marshalGoalSnapshot(t.controller.GoalSnapshot())
	case "create":
		return t.create(args)
	case "update":
		return t.update(args)
	case "clear":
		if args.hasStateArguments() {
			return "", fmt.Errorf("goal_control clear accepts only the action argument")
		}
		t.controller.ClearGoal()
		return marshalGoalSnapshot(t.controller.GoalSnapshot())
	case "":
		return "", fmt.Errorf("goal_control requires an action")
	default:
		return "", fmt.Errorf("goal_control action %q is invalid; use get, create, update, or clear", args.Action)
	}
}

func decodeGoalControlArgs(raw json.RawMessage) (goalControlArgs, error) {
	var args goalControlArgs
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return goalControlArgs{}, fmt.Errorf("goal_control invalid arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return goalControlArgs{}, fmt.Errorf("goal_control invalid arguments: multiple JSON values")
		}
		return goalControlArgs{}, fmt.Errorf("goal_control invalid arguments: %w", err)
	}
	return args, nil
}

func (a goalControlArgs) hasStateArguments() bool {
	return a.Objective != nil || a.Strict != nil || a.ResearchMode != nil ||
		a.TurnBudget != nil || a.TokenBudget != nil || a.TimeBudgetSeconds != nil ||
		a.Status != nil || a.Reason != nil
}

func (t *goalControlTool) create(args goalControlArgs) (string, error) {
	if args.Status != nil || args.Reason != nil {
		return "", fmt.Errorf("goal_control create does not accept status or reason")
	}
	if args.Objective == nil || strings.TrimSpace(*args.Objective) == "" {
		return "", fmt.Errorf("goal_control create requires a non-empty objective")
	}

	options := GoalOptions{ResearchMode: GoalResearchAuto}
	if args.Strict != nil {
		options.Strict = *args.Strict
	}
	if args.ResearchMode != nil {
		mode, err := parseGoalResearchMode(*args.ResearchMode)
		if err != nil {
			return "", err
		}
		options.ResearchMode = mode
	}
	if args.TurnBudget != nil {
		if *args.TurnBudget <= 0 {
			return "", fmt.Errorf("goal_control create turn_budget must be greater than zero")
		}
		options.TurnBudget = *args.TurnBudget
	}
	if args.TokenBudget != nil {
		if *args.TokenBudget < 0 {
			return "", fmt.Errorf("goal_control create token_budget must be non-negative")
		}
		options.TokenBudget = *args.TokenBudget
	}
	if args.TimeBudgetSeconds != nil {
		if *args.TimeBudgetSeconds < 0 {
			return "", fmt.Errorf("goal_control create time_budget_seconds must be non-negative")
		}
		options.TimeBudgetSeconds = *args.TimeBudgetSeconds
	}

	path, data, ok, err := t.controller.goals.createFromTool(
		strings.TrimSpace(*args.Objective), options, t.controller.goalTodos(),
	)
	if err != nil {
		return "", err
	}
	t.controller.persistGoalState(path, data, ok)
	created := t.controller.GoalSnapshot()
	t.controller.activateGoalUsageRound(created.Generation)
	return marshalGoalSnapshot(created)
}

func (t *goalControlTool) update(args goalControlArgs) (string, error) {
	if args.Objective != nil || args.Strict != nil || args.ResearchMode != nil ||
		args.TurnBudget != nil || args.TokenBudget != nil || args.TimeBudgetSeconds != nil {
		return "", fmt.Errorf("goal_control update accepts only status and reason")
	}
	if args.Status == nil || strings.TrimSpace(*args.Status) == "" {
		return "", fmt.Errorf("goal_control update requires status complete or blocked")
	}

	status := strings.ToLower(strings.TrimSpace(*args.Status))
	reason := ""
	switch status {
	case GoalStatusComplete:
		if args.Reason != nil {
			return "", fmt.Errorf("goal_control update complete does not accept a reason")
		}
	case GoalStatusBlocked:
		if args.Reason == nil || strings.TrimSpace(*args.Reason) == "" {
			return "", fmt.Errorf("goal_control update blocked requires a non-empty reason")
		}
		reason = cleanGoalBlockReason(*args.Reason)
		if reason == "" {
			return "", fmt.Errorf("goal_control update blocked requires a non-empty reason")
		}
	default:
		return "", fmt.Errorf("goal_control update status %q is invalid; use complete or blocked", *args.Status)
	}
	started := t.controller.GoalSnapshot()
	if started.Status != GoalStatusRunning || strings.TrimSpace(started.Goal) == "" {
		current := started.Status
		if current == "" {
			current = GoalStatusStopped
		}
		return "", fmt.Errorf("goal_control cannot update a goal in %s status; create a running goal first", current)
	}
	todos := t.controller.goalTodos()
	if status == GoalStatusComplete {
		if t.controller.executor == nil {
			return "", fmt.Errorf("goal_control cannot complete a goal without an executor")
		}
		failure := formatIncompleteTodos(todos, t.controller.executor.GoalReadinessFailure())
		if failure = strings.TrimSpace(failure); failure != "" {
			return "", fmt.Errorf("goal_control cannot complete the goal: %s", failure)
		}
	}
	if !t.controller.claimStructuredGoalSignal(started.Generation) {
		return "", fmt.Errorf("goal_control already accepted a structured status report for this goal turn")
	}

	path, data, ok, err := t.controller.goals.updateFromTool(
		started.Generation, status, reason, todos,
	)
	if err != nil {
		t.controller.releaseStructuredGoalSignal(started.Generation)
		return "", err
	}
	t.controller.persistGoalState(path, data, ok)
	updated := t.controller.GoalSnapshot()
	return marshalGoalSnapshot(updated)
}

func parseGoalResearchMode(raw string) (GoalResearchMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto":
		return GoalResearchAuto, nil
	case "on":
		return GoalResearchOn, nil
	case "off":
		return GoalResearchOff, nil
	default:
		return GoalResearchAuto, fmt.Errorf("goal_control create research_mode %q is invalid; use auto, on, or off", raw)
	}
}

func marshalGoalSnapshot(snapshot GoalSnapshot) (string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("goal_control encode snapshot: %w", err)
	}
	return string(data), nil
}

// createFromTool differs from the legacy setter by refusing to replace a
// running generation. A structured create is an explicit state transition, not
// an implicit cancel-and-replace operation.
func (g *goalMachine) createFromTool(objective string, options GoalOptions, todos []evidence.TodoItem) (string, []byte, bool, error) {
	objective = strings.TrimSpace(objective)
	options = normalizeGoalOptions(options)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.status == GoalStatusRunning {
		return "", nil, false, fmt.Errorf("goal_control cannot create a goal while goal %q is running", g.objective)
	}

	g.startGoalLocked(objective, options, time.Now().UTC())
	path, data, ok := g.buildStateLocked(todos)
	return path, data, ok, nil
}

// updateFromTool applies an authoritative structured status report. Completion
// is terminal immediately after the caller's readiness gate; blocked reports
// share the same three-consecutive-report audit as marker-driven updates.
func (g *goalMachine) updateFromTool(generation uint64, status, reason string, todos []evidence.TodoItem) (string, []byte, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.generation != generation {
		return "", nil, false, fmt.Errorf("goal_control goal changed while the update was being checked; get the current state and retry")
	}
	if g.status != GoalStatusRunning || strings.TrimSpace(g.goal) == "" {
		current := g.status
		if current == "" {
			current = GoalStatusStopped
		}
		return "", nil, false, fmt.Errorf("goal_control cannot update a goal in %s status; create a running goal first", current)
	}

	if status != GoalStatusComplete && status != GoalStatusBlocked {
		return "", nil, false, fmt.Errorf("goal_control internal invalid status %q", status)
	}
	now := time.Now().UTC()
	g.updateElapsedLocked(now)
	g.interceptMsg, g.intercepts = "", 0
	g.idleTurns = 0
	g.turns++
	switch status {
	case GoalStatusComplete:
		g.blocks, g.block = 0, ""
		if g.strict && !g.selfCheckDone {
			g.selfCheckDone = true
			g.interceptMsg = goalSelfCheckTurn
			break
		}
		g.status = GoalStatusComplete
		g.terminalAt = now
		g.goal = ""
		g.selfCheckDone = false
	case GoalStatusBlocked:
		g.selfCheckDone = false
		g.recordBlockedLocked(reason, now)
	}
	if g.status == GoalStatusRunning {
		if budgetReason := g.budgetReasonLocked(now, true); budgetReason != "" {
			g.markBudgetLimitedLocked(budgetReason, now)
		}
	}
	path, data, ok := g.buildStateLocked(todos)
	return path, data, ok, nil
}
