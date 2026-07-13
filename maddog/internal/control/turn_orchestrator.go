package control

import (
	"context"
	"errors"
	"strings"

	"maddog/internal/agent"
	"maddog/internal/jobs"
	"maddog/internal/provider"
)

// turnOrchestrator owns foreground turn execution while Controller keeps the
// public ports, run-state guard, and session-scoped dependencies.
type turnOrchestrator struct {
	c        *Controller
	goalTurn goalTurnObservation
}

type orchestratedTurn struct {
	input          string
	raw            string
	display        string
	editedOriginal string
	synthetic      bool
	headless       bool
}

// goalTurnObservation is scoped to one Runner.Run unit. Keeping this separate
// from the whole transcript prevents a hook-blocked/no-op run from replaying an
// older assistant marker, and lets idle detection see tools called before the
// final assistant response.
type goalTurnObservation struct {
	assistantText string
	hasAssistant  bool
	toolCalled    bool
	generation    uint64
	wasRunning    bool
	unitRan       bool
}

type goalTurnCursor struct {
	messages   int
	rewrite    int
	generation uint64
	wasRunning bool
}

func newTurnOrchestrator(c *Controller) *turnOrchestrator {
	return &turnOrchestrator{c: c}
}

func (o *turnOrchestrator) runTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display})
}

func (o *turnOrchestrator) runEditedTurnWithRawDisplay(ctx context.Context, input, raw, display, original string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display, editedOriginal: original})
}

func (o *turnOrchestrator) runSyntheticTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display, synthetic: true})
}

func (o *turnOrchestrator) runHeadlessTurn(ctx context.Context, input, raw string, synthetic bool) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, synthetic: synthetic, headless: true})
}

func (o *turnOrchestrator) runComposedSyntheticTurn(ctx context.Context, text string) error {
	c := o.c
	if !c.checkGoalBudget() {
		return nil
	}
	c.beginGoalUsageRound()
	defer c.endGoalUsageRound()
	return c.runner.Run(agent.WithMemoryCompilerSkip(ctx), c.ComposeSynthetic(text))
}

func (o *turnOrchestrator) runOrchestratedTurn(ctx context.Context, turn orchestratedTurn) error {
	c := o.c
	o.goalTurn = goalTurnObservation{}
	c.resetStructuredGoalSignal()
	c.maybeSessionStart(ctx)
	if !turn.synthetic && !turn.headless {
		c.maybeAutoPlan(ctx, turn.raw)
	}
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, c.inputImages(turn.input))
	if !turn.synthetic {
		turn.input = c.withImageFallback(ctx, turn.input)
	}
	// Synthetic, controller-injected turns (goal-loop continuation,
	// plan-approved execution, …) must not be Memory v5-compiled: compiling them
	// re-injects a contract the model echoes back, which spins the goal loop
	// forever (#5342, #5329). Only genuine user turns supply a compiler source.
	if turn.synthetic || IsSyntheticUserMessage(turn.raw) {
		ctx = agent.WithMemoryCompilerSkip(ctx)
	} else if !turn.headless {
		ctx = agent.WithMemoryCompilerSourceInput(ctx, turn.raw)
	}
	input := c.Compose(turn.input)
	if !turn.synthetic {
		input = c.orchestrateSkills(ctx, input)
	}
	if !c.checkGoalBudget() {
		return nil
	}
	startMessages := c.messageCount()
	cursor := o.goalTurnCursor(startMessages)
	ran := false
	defer func() {
		if ran {
			o.goalTurn = o.observeGoalTurn(cursor)
		}
	}()
	defer c.snapshotActivityIfChanged(startMessages)
	defer c.recordDisplayForNewUser(startMessages, turn.display)
	if turn.editedOriginal != "" {
		defer c.markEditedForNewUser(startMessages, turn.editedOriginal)
	}
	// Open a checkpoint only for visible user turns before the user message is
	// appended, so the recorded message boundary precedes it and pre-edit
	// snapshots land here. Synthetic continuations stay attached to the visible
	// turn that spawned them; otherwise hidden user-role messages would advance
	// backend checkpoint turns without a matching frontend turn.
	if !turn.synthetic && !turn.headless {
		c.beginCheckpoint(input)
	}
	if c.guardianSess != nil {
		c.guardianSess.ResetTurn()
	}
	// UserPromptSubmit / Stop hooks bracket the whole turn (incl. the plan
	// research + approved-execution sub-turns below): a gating UserPromptSubmit
	// aborts before any model call; Stop fires once when the turn returns.
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil // the hook's notify callback already surfaced the reason
		}
		defer func() { c.hooks.Stop(context.Background(), lastAssistantText(c.History()), turn) }()
	}
	c.markInFlightTurn(startMessages, !turn.synthetic && !IsSyntheticUserMessage(turn.raw))
	ran = true
	c.beginGoalUsageRound()
	defer c.endGoalUsageRound()
	err := c.runner.Run(ctx, input)
	if err == nil {
		c.clearInFlightTurn()
	} else {
		// When the user explicitly cancels (Ctrl+C), the incomplete turn's
		// assistant messages and tool results are already saved to the
		// session. If they stay, the next turn's model sees leftover
		// in-progress todo items and partial tool calls and may re-execute
		// the interrupted work. Keep the real user prompt for visible turns so
		// follow-up questions and resumes do not lose the user's context (#5499).
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			if turn.synthetic || IsSyntheticUserMessage(turn.raw) {
				c.stripTurnMessagesAfter(startMessages)
			} else {
				c.stripCancelledVisibleTurnMessagesAfter(startMessages)
			}
		}
		c.clearInFlightTurn()
		return err
	}
	if turn.headless {
		return nil
	}
	c.mu.Lock()
	plan := c.planMode
	c.mu.Unlock()
	if !plan {
		return nil
	}
	proposal := lastAssistantText(c.History())
	if proposal == "" {
		return nil // no substantive proposal to gate
	}
	// The plan is already visible as the assistant's answer, so the request
	// carries no subject — it's purely the gate.
	allow, _, err := c.requestApproval(ctx, planApprovalTool, "", nil)
	if err != nil {
		return err
	}
	if !allow {
		return nil // keep planning; plan mode stays on
	}
	c.SetPlanMode(false)
	todoArgs := c.seedPlanTodos(proposal)
	execStart := c.sessionMessageCount()
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only; a
	// later turn (even "continue") falls back to the normal per-tool approval.
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	err = func() error {
		c.markInFlightTurn(execStart, false)
		defer c.clearInFlightTurn()
		return o.runComposedSyntheticTurn(ctx, planApprovedMessage)
	}()
	if err != nil {
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			c.stripTurnMessagesAfter(execStart)
		}
		return err
	}
	if todoArgs != "" && !c.hasTodoUpdateSince(execStart) {
		c.completePlanTodos(todoArgs)
	}
	return nil
}

func (o *turnOrchestrator) goalTurnCursor(messages int) goalTurnCursor {
	snapshot := o.c.GoalSnapshot()
	cursor := goalTurnCursor{
		messages:   messages,
		generation: snapshot.Generation,
		wasRunning: snapshot.Status == GoalStatusRunning,
	}
	if o.c.executor != nil && o.c.executor.Session() != nil {
		cursor.rewrite = o.c.executor.Session().RewriteVersion()
	}
	return cursor
}

func (o *turnOrchestrator) observeGoalTurn(cursor goalTurnCursor) goalTurnObservation {
	c := o.c
	msgs := c.History()
	start := cursor.messages
	rewritten := false
	if c.executor != nil && c.executor.Session() != nil {
		rewritten = c.executor.Session().RewriteVersion() != cursor.rewrite
	}
	if start < 0 {
		start = 0
	}
	if rewritten || start > len(msgs) {
		// Agent compaction rewrites the prefix but retains the just-finished
		// assistant in its recent tail. The per-run evidence ledger remains a
		// precise source for tool activity across that rewrite.
		start = 0
	}

	obs := goalTurnObservation{generation: cursor.generation, wasRunning: cursor.wasRunning, unitRan: true}
	for _, msg := range msgs[start:] {
		if msg.Role != provider.RoleAssistant {
			continue
		}
		if !rewritten && len(msg.ToolCalls) > 0 {
			obs.toolCalled = true
		}
		if strings.TrimSpace(msg.Content) != "" {
			obs.assistantText = msg.Content
			obs.hasAssistant = true
		}
	}
	if rewritten && c.executor != nil {
		for _, receipt := range c.executor.EvidenceReceipts() {
			if receipt.ToolName != "" && receipt.ToolName != "goal_control" {
				obs.toolCalled = true
				break
			}
		}
	}
	return obs
}

func (o *turnOrchestrator) runGoalLoopWithRawDisplay(ctx context.Context, input, raw, display string) error {
	defer o.c.cleanupGeneratedRuntimeSkills()
	startMessages := o.c.messageCount()
	if err := o.runTurnWithRawDisplay(ctx, input, raw, display); err != nil {
		o.handleGoalTurnError(err)
		return err
	}
	if err := o.continueGoal(ctx, false); err != nil {
		return err
	}
	o.c.captureReplayBundle(raw, startMessages)
	return nil
}

func (o *turnOrchestrator) runHeadlessGoalLoop(ctx context.Context, input string) error {
	defer o.c.cleanupGeneratedRuntimeSkills()
	startMessages := o.c.messageCount()
	if err := o.runHeadlessTurn(ctx, input, input, false); err != nil {
		o.handleGoalTurnError(err)
		return err
	}
	if err := o.continueGoal(ctx, true); err != nil {
		return err
	}
	o.c.captureReplayBundle(input, startMessages)
	return nil
}

func (o *turnOrchestrator) runEditedGoalLoopWithRawDisplay(ctx context.Context, input, raw, display, original string) error {
	defer o.c.cleanupGeneratedRuntimeSkills()
	startMessages := o.c.messageCount()
	if err := o.runEditedTurnWithRawDisplay(ctx, input, raw, display, original); err != nil {
		o.handleGoalTurnError(err)
		return err
	}
	if err := o.continueGoal(ctx, false); err != nil {
		return err
	}
	o.c.captureReplayBundle(raw, startMessages)
	return nil
}

func (o *turnOrchestrator) continueGoal(ctx context.Context, headless bool) error {
	c := o.c
	for {
		cont := o.advanceGoalAfterTurn()
		if !cont {
			// Usage is reported before the final assistant marker. A normal
			// advance settles the budget itself; no-assistant and stale-marker
			// units still need the post-unit gate.
			c.checkGoalBudget()
			return nil
		}
		if err := ctx.Err(); err != nil {
			o.handleGoalTurnError(err)
			return err
		}
		turn := goalContinueTurn
		if msg, ok := c.goals.takeIntercept(); ok {
			turn = msg
			c.notice("goal intercept: incomplete todos or readiness checks remain")
		}
		var err error
		if headless {
			err = o.runHeadlessTurn(ctx, turn, turn, true)
		} else {
			err = o.runSyntheticTurnWithRawDisplay(ctx, turn, turn, "")
		}
		if err != nil {
			o.handleGoalTurnError(err)
			return err
		}
	}
}

func (o *turnOrchestrator) handleGoalTurnError(err error) {
	if err == nil {
		return
	}
	if o.c.CancelRequested() {
		o.c.stopGoal(GoalStatusStopped)
		return
	}
	o.c.interruptGoal(err)
}

func (o *turnOrchestrator) advanceGoalAfterTurn() bool {
	c := o.c
	snapshot := c.GoalSnapshot()
	if c.consumeStructuredGoalSignal(snapshot.Generation) {
		return snapshot.Status == GoalStatusRunning
	}
	generation := o.goalTurn.generation
	createdDuringUnit := o.goalTurn.unitRan && !o.goalTurn.wasRunning &&
		snapshot.Status == GoalStatusRunning && snapshot.Generation != generation
	if createdDuringUnit {
		if !o.goalTurn.hasAssistant {
			return true
		}
		generation = snapshot.Generation
	}
	if !o.goalTurn.hasAssistant {
		return false
	}
	// Gather every input the FSM needs off the goal lock: parse the marker,
	// snapshot the executor's todos + readiness, and check tool activity. None
	// of these touch goal state, so the machine's critical section stays pure.
	status, reason, _ := parseGoalStatusMarker(o.goalTurn.assistantText)
	var readiness string
	if c.executor != nil {
		readiness = c.executor.GoalReadinessFailure()
	}
	res := c.goals.advance(goalAdvanceInput{
		generation: generation,
		status:     status,
		reason:     reason,
		toolCalled: o.goalTurn.toolCalled,
		todos:      c.goalTodos(),
		readiness:  readiness,
	})
	c.persistGoalState(res.path, res.data, res.ok)
	if res.notice != "" {
		c.notice(res.notice)
	}
	if (res.cont || res.override) && c.executor != nil {
		c.executor.RecordControlSignal(res.controlSignal)
	}
	return res.cont
}
