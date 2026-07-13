package control

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"maddog/internal/evidence"
	"maddog/internal/fileutil"
	"maddog/internal/store"
)

const (
	// GoalSnapshotSchemaVersion is the current durable goal sidecar schema.
	GoalSnapshotSchemaVersion = 1
	// GoalModeAutonomous identifies the controller-managed continuation mode.
	GoalModeAutonomous = "goal"
	// GoalStatusBudgetLimited is terminal when a configured goal budget is exhausted.
	GoalStatusBudgetLimited = "budget_limited"

	defaultGoalTurnBudget = 50
	// maxGoalAutoTurns is kept as a package-local compatibility alias for tests
	// and diagnostics that referred to the legacy hard cap.
	maxGoalAutoTurns   = defaultGoalTurnBudget
	maxGoalIdleTurns   = 2
	goalContinueTurn   = "Continue pursuing the active goal. If it is complete, provide the concise final result and end with [goal:complete]. If it is truly blocked on a user-owned decision after trying sensible defaults, end with [goal:blocked:<short reason>]. Otherwise do the next useful work and end with [goal:continue]."
	goalSelfCheckTurn  = "The agent signaled goal completion and all tasks are marked done. Before finalizing, perform a brief quality self-check:\n1. Verify any changed files compile or parse correctly\n2. Run the relevant tests if applicable\n3. Confirm the original requirements are met\nIf everything checks out, signal [goal:complete]. If issues are found, fix them and signal [goal:complete] when done."
	goalCompleteNotice = "goal complete"
)

// goalMachine owns the active goal's finite-state machine and its persistence.
// It is a strict leaf: its methods take only the machine's own locks and never
// call back into the Controller, so the controller may hold c.mu while invoking
// a getter without risking lock inversion. The FSM is pure — advance() takes
// already-gathered inputs (the parsed marker, the executor's todo snapshot and
// readiness, whether a tool ran) and returns what to persist plus a notice, so
// no disk or executor work happens under mu.
type goalMachine struct {
	// mu guards the FSM fields below; every critical section under it is short
	// and non-blocking (no disk I/O, no executor calls).
	mu            sync.Mutex
	goal          string // legacy/publicly visible goal; terminal completion clears it
	objective     string // durable identity retained after terminal transitions
	id            string
	mode          string
	status        string
	researchMode  GoalResearchMode
	turns         int
	blocks        int
	block         string
	interceptMsg  string
	intercepts    int
	strict        bool
	selfCheckDone bool
	idleTurns     int
	turnBudget    int
	tokenBudget   int64
	tokensUsed    int64
	timeBudget    int64
	timeUsed      int64
	lastError     string
	interruptedAt time.Time
	generation    uint64
	revision      uint64
	createdAt     time.Time
	startedAt     time.Time
	updatedAt     time.Time
	terminalAt    time.Time
	todos         []evidence.TodoItem

	// statePath is the persisted goal-state sidecar; empty disables persistence.
	statePath string
	// writeMu serializes goal-state disk writes so concurrent saves don't
	// interleave or land out of order. Taken OFF mu by writeState.
	writeMu sync.Mutex
}

// GoalSnapshot is the durable, versioned representation of one session's most
// recent goal. Objective and ID survive terminal transitions; Goal preserves the
// historical public Goal() semantics (completion and explicit clear hide it).
// Generation identifies replacements, while Revision orders mutations within a
// generation so a delayed writer cannot overwrite newer state.
type GoalSnapshot struct {
	SchemaVersion     int                 `json:"schemaVersion"`
	ID                string              `json:"id,omitempty"`
	Objective         string              `json:"objective,omitempty"`
	Goal              string              `json:"goal,omitempty"`
	Status            string              `json:"status,omitempty"`
	Mode              string              `json:"mode,omitempty"`
	ResearchMode      GoalResearchMode    `json:"researchMode,omitempty"`
	Strict            bool                `json:"strict,omitempty"`
	Turns             int                 `json:"turns,omitempty"`
	Blocks            int                 `json:"blocks,omitempty"`
	Block             string              `json:"block,omitempty"`
	InterceptMsg      string              `json:"interceptMsg,omitempty"`
	Intercepts        int                 `json:"intercepts,omitempty"`
	SelfCheckDone     bool                `json:"selfCheckDone,omitempty"`
	IdleTurns         int                 `json:"idleTurns,omitempty"`
	TurnBudget        int                 `json:"turnBudget,omitempty"`
	TokenBudget       int64               `json:"tokenBudget,omitempty"`
	TokensUsed        int64               `json:"tokensUsed,omitempty"`
	TimeBudgetSeconds int64               `json:"timeBudgetSeconds,omitempty"`
	TimeUsedSeconds   int64               `json:"timeUsedSeconds,omitempty"`
	LastError         string              `json:"lastError,omitempty"`
	InterruptedAt     time.Time           `json:"interruptedAt,omitzero"`
	Generation        uint64              `json:"generation,omitempty"`
	Revision          uint64              `json:"revision,omitempty"`
	CreatedAt         time.Time           `json:"createdAt,omitzero"`
	StartedAt         time.Time           `json:"startedAt,omitzero"`
	UpdatedAt         time.Time           `json:"updatedAt,omitzero"`
	TerminalAt        time.Time           `json:"terminalAt,omitzero"`
	Todos             []evidence.TodoItem `json:"todos,omitempty"`
}

// GoalOptions configures one autonomous goal run. TurnBudget defaults to 50
// when it is zero or negative; zero token and time budgets are unlimited.
// ResearchMode keeps the existing auto/on/off composition behavior.
type GoalOptions struct {
	ResearchMode      GoalResearchMode
	TurnBudget        int
	TokenBudget       int64
	TimeBudgetSeconds int64
	Strict            bool
}

// goalState keeps package-local compatibility with existing tests and legacy
// helpers while the public snapshot becomes the canonical persisted contract.
type goalState = GoalSnapshot

var goalStatePathLocks sync.Map // map[string]*sync.Mutex

// goalAdvanceInput carries everything the FSM needs for one continuation step,
// gathered by the caller off the machine's lock.
type goalAdvanceInput struct {
	generation uint64 // goal generation captured before the provider turn started
	status     string // parsed marker status ("" when the turn carried no marker)
	reason     string // blocked reason from the marker, if any
	toolCalled bool   // whether the last turn made any tool call
	todos      []evidence.TodoItem
	readiness  string // executor.GoalReadinessFailure()
}

// goalAdvanceResult reports the FSM step's outcome. data/path/ok describe the
// state to persist (built under mu when something changed); notice is surfaced
// to the user; cont reports whether the goal loop should continue.
type goalAdvanceResult struct {
	notice        string
	cont          bool
	override      bool
	controlSignal evidence.FailureSignal
	path          string
	data          []byte
	ok            bool
}

type goalBudgetResult struct {
	allowed bool
	notice  string
	path    string
	data    []byte
	ok      bool
}

// goalStatePath derives a session's persisted goal-state sidecar.
func goalStatePath(sessionPath string) string {
	return store.SessionGoalState(sessionPath)
}

func goalPathLock(path string) *sync.Mutex {
	lock, _ := goalStatePathLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func newGoalID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return fmt.Sprintf("%x", id[:])
	}
	return fmt.Sprintf("goal-%d", time.Now().UTC().UnixNano())
}

func legacyGoalID(path, objective string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + objective))
	return fmt.Sprintf("legacy-%x", sum[:12])
}

func normalizeGoalOptions(options GoalOptions) GoalOptions {
	if options.TurnBudget <= 0 {
		options.TurnBudget = defaultGoalTurnBudget
	}
	if options.TokenBudget < 0 {
		options.TokenBudget = 0
	}
	if options.TimeBudgetSeconds < 0 {
		options.TimeBudgetSeconds = 0
	}
	return options
}

func validGoalStatus(status string) bool {
	switch status {
	case "", GoalStatusRunning, GoalStatusComplete, GoalStatusBlocked, GoalStatusStopped, GoalStatusBudgetLimited:
		return true
	default:
		return false
	}
}

func readGoalSnapshot(path string) (GoalSnapshot, bool, error) {
	if strings.TrimSpace(path) == "" {
		return GoalSnapshot{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return GoalSnapshot{}, false, nil
		}
		return GoalSnapshot{}, false, fmt.Errorf("read goal snapshot %q: %w", path, err)
	}
	var state GoalSnapshot
	if err := json.Unmarshal(data, &state); err != nil {
		return GoalSnapshot{}, false, fmt.Errorf("decode goal snapshot %q: %w", path, err)
	}
	if state.SchemaVersion > GoalSnapshotSchemaVersion {
		return GoalSnapshot{}, false, fmt.Errorf("unsupported goal snapshot schema %d", state.SchemaVersion)
	}
	if !validGoalStatus(state.Status) {
		return GoalSnapshot{}, false, fmt.Errorf("invalid goal status %q", state.Status)
	}
	if state.Objective == "" {
		state.Objective = strings.TrimSpace(state.Goal)
	}
	if state.Status == "" {
		if state.Objective == "" {
			state.Status = GoalStatusStopped
		} else {
			state.Status = GoalStatusRunning
		}
	}
	if state.Status == GoalStatusRunning && state.Goal == "" {
		state.Goal = state.Objective
	}
	if state.Objective != "" {
		if state.ID == "" {
			state.ID = legacyGoalID(path, state.Objective)
		}
		if state.Mode == "" {
			state.Mode = GoalModeAutonomous
		}
		if state.Generation == 0 {
			state.Generation = 1
		}
	}
	if state.Revision == 0 {
		state.Revision = 1
	}
	if state.Objective != "" {
		if state.TurnBudget <= 0 {
			state.TurnBudget = defaultGoalTurnBudget
		}
		if state.TokenBudget < 0 {
			state.TokenBudget = 0
		}
		if state.TokensUsed < 0 {
			state.TokensUsed = 0
		}
		if state.TimeBudgetSeconds < 0 {
			state.TimeBudgetSeconds = 0
		}
		if state.TimeUsedSeconds < 0 {
			state.TimeUsedSeconds = 0
		}
	}
	if state.CreatedAt.IsZero() {
		if info, statErr := os.Stat(path); statErr == nil {
			state.CreatedAt = info.ModTime().UTC()
		} else {
			state.CreatedAt = time.Now().UTC()
		}
	}
	if state.StartedAt.IsZero() && state.Objective != "" {
		state.StartedAt = state.CreatedAt
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = state.CreatedAt
	}
	state.SchemaVersion = GoalSnapshotSchemaVersion
	state.Todos = append([]evidence.TodoItem(nil), state.Todos...)
	return state, true, nil
}

// bindStatePath replaces the complete in-memory FSM with the target session's
// snapshot. Missing or corrupt target state resets the machine, preventing a
// goal from the previously bound session leaking across Resume/switch. adopt
// preserves a goal created before the first session path was assigned.
func (g *goalMachine) bindStatePath(path string, load, adopt bool) bool {
	var loaded GoalSnapshot
	found := false
	if load {
		var err error
		loaded, found, err = readGoalSnapshot(path)
		if err != nil {
			slog.Warn("controller: load goal state", "path", path, "err", err)
			found = false
		}
	} else if path != "" {
		pathLock := goalPathLock(path)
		pathLock.Lock()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("controller: clear rebound goal state", "path", path, "err", err)
		}
		pathLock.Unlock()
	}

	g.mu.Lock()
	oldPath := g.statePath
	if found {
		g.applySnapshotLocked(loaded, path)
		g.mu.Unlock()
		return true
	}
	if adopt && oldPath == "" && strings.TrimSpace(g.objective) != "" {
		g.statePath = path
		persistPath, data, ok := g.buildStateLocked(g.todos)
		g.mu.Unlock()
		if ok {
			g.writeState(persistPath, data)
		}
		return false
	}
	g.resetLocked(path)
	g.mu.Unlock()
	return false
}

func (g *goalMachine) resetLocked(path string) {
	g.goal, g.objective, g.id, g.mode, g.status = "", "", "", "", ""
	g.researchMode = GoalResearchAuto
	g.turns, g.blocks, g.block = 0, 0, ""
	g.interceptMsg, g.intercepts = "", 0
	g.strict, g.selfCheckDone, g.idleTurns = false, false, 0
	g.turnBudget, g.tokenBudget, g.tokensUsed = 0, 0, 0
	g.timeBudget, g.timeUsed = 0, 0
	g.lastError, g.interruptedAt = "", time.Time{}
	g.generation, g.revision = 0, 0
	g.createdAt, g.startedAt, g.updatedAt, g.terminalAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	g.todos = nil
	g.statePath = path
}

func (g *goalMachine) applySnapshotLocked(state GoalSnapshot, path string) {
	g.goal, g.objective, g.id, g.mode = state.Goal, state.Objective, state.ID, state.Mode
	g.status, g.researchMode, g.strict = state.Status, state.ResearchMode, state.Strict
	g.turns, g.blocks, g.block = state.Turns, state.Blocks, state.Block
	g.interceptMsg, g.intercepts = state.InterceptMsg, state.Intercepts
	g.selfCheckDone, g.idleTurns = state.SelfCheckDone, state.IdleTurns
	g.turnBudget, g.tokenBudget, g.tokensUsed = state.TurnBudget, state.TokenBudget, state.TokensUsed
	g.timeBudget, g.timeUsed = state.TimeBudgetSeconds, state.TimeUsedSeconds
	g.lastError, g.interruptedAt = state.LastError, state.InterruptedAt
	g.generation, g.revision = state.Generation, state.Revision
	g.createdAt, g.startedAt, g.updatedAt, g.terminalAt = state.CreatedAt, state.StartedAt, state.UpdatedAt, state.TerminalAt
	g.todos = append([]evidence.TodoItem(nil), state.Todos...)
	g.statePath = path
}

func (g *goalMachine) snapshotLocked() GoalSnapshot {
	return GoalSnapshot{
		SchemaVersion:     GoalSnapshotSchemaVersion,
		ID:                g.id,
		Objective:         g.objective,
		Goal:              g.goal,
		Status:            g.status,
		Mode:              g.mode,
		ResearchMode:      g.researchMode,
		Strict:            g.strict,
		Turns:             g.turns,
		Blocks:            g.blocks,
		Block:             g.block,
		InterceptMsg:      g.interceptMsg,
		Intercepts:        g.intercepts,
		SelfCheckDone:     g.selfCheckDone,
		IdleTurns:         g.idleTurns,
		TurnBudget:        g.turnBudget,
		TokenBudget:       g.tokenBudget,
		TokensUsed:        g.tokensUsed,
		TimeBudgetSeconds: g.timeBudget,
		TimeUsedSeconds:   g.timeUsed,
		LastError:         g.lastError,
		InterruptedAt:     g.interruptedAt,
		Generation:        g.generation,
		Revision:          g.revision,
		CreatedAt:         g.createdAt,
		StartedAt:         g.startedAt,
		UpdatedAt:         g.updatedAt,
		TerminalAt:        g.terminalAt,
		Todos:             append([]evidence.TodoItem(nil), g.todos...),
	}
}

func (g *goalMachine) durableSnapshot() GoalSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked()
}

// snapshot returns the fields Compose injects into outgoing turns.
func (g *goalMachine) snapshot() (goal, status string, mode GoalResearchMode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.goal, g.status, g.researchMode
}

func (g *goalMachine) goalText() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.goal
}

// active reports whether a goal is currently running.
func (g *goalMachine) active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.TrimSpace(g.goal) != "" && g.status == GoalStatusRunning
}

// statusForDisplay maps the empty zero status to "stopped" for frontends.
func (g *goalMachine) statusForDisplay() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.status == "" {
		return GoalStatusStopped
	}
	return g.status
}

// set installs a session-scoped goal (or clears it when goal is empty), resets
// the per-goal counters, and returns the state to persist. ok is false (no
// persistence) when the goal is unchanged or no state path is configured.
func (g *goalMachine) set(goal string, mode GoalResearchMode, todos []evidence.TodoItem) (string, []byte, bool) {
	return g.setWithOptionsInternal(goal, GoalOptions{ResearchMode: mode}, todos, true)
}

func (g *goalMachine) setWithOptions(goal string, options GoalOptions, todos []evidence.TodoItem) (string, []byte, bool) {
	return g.setWithOptionsInternal(goal, options, todos, false)
}

func (g *goalMachine) resetProgressLocked(strict bool) {
	g.turns, g.blocks, g.block = 0, 0, ""
	g.interceptMsg, g.intercepts = "", 0
	g.selfCheckDone, g.idleTurns, g.strict = false, 0, strict
}

func (g *goalMachine) startGoalLocked(objective string, options GoalOptions, now time.Time) {
	g.resetProgressLocked(options.Strict)
	g.generation++
	g.revision = 0
	g.goal, g.objective, g.id = objective, objective, newGoalID()
	g.mode, g.status, g.researchMode = GoalModeAutonomous, GoalStatusRunning, options.ResearchMode
	g.turnBudget, g.tokenBudget, g.tokensUsed = options.TurnBudget, options.TokenBudget, 0
	g.timeBudget, g.timeUsed = options.TimeBudgetSeconds, 0
	g.lastError, g.interruptedAt = "", time.Time{}
	g.createdAt, g.startedAt, g.terminalAt = now, now, time.Time{}
}

func (g *goalMachine) setWithOptionsInternal(goal string, options GoalOptions, todos []evidence.TodoItem, legacy bool) (string, []byte, bool) {
	goal = strings.TrimSpace(goal)
	options = normalizeGoalOptions(options)
	g.mu.Lock()
	defer g.mu.Unlock()
	if goal != "" && g.goal == goal && g.status == GoalStatusRunning &&
		g.researchMode == options.ResearchMode && (legacy ||
		(g.strict == options.Strict && g.turnBudget == options.TurnBudget &&
			g.tokenBudget == options.TokenBudget && g.timeBudget == options.TimeBudgetSeconds)) {
		return "", nil, false
	}
	if goal == "" {
		g.resetProgressLocked(options.Strict)
		g.goal, g.status, g.researchMode = "", GoalStatusStopped, GoalResearchAuto
		if g.objective != "" {
			now := time.Now().UTC()
			g.updateElapsedLocked(now)
			g.terminalAt = now
		}
	} else {
		g.startGoalLocked(goal, options, time.Now().UTC())
	}
	return g.buildStateLocked(todos)
}

func (g *goalMachine) setStrict(strict bool, todos []evidence.TodoItem) (string, []byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.strict == strict {
		return "", nil, false
	}
	g.strict = strict
	return g.buildStateLocked(todos)
}

func (g *goalMachine) updateElapsedLocked(now time.Time) bool {
	if g.startedAt.IsZero() || now.Before(g.startedAt) {
		return false
	}
	seconds := int64(now.Sub(g.startedAt) / time.Second)
	if seconds <= g.timeUsed {
		return false
	}
	g.timeUsed = seconds
	return true
}

func (g *goalMachine) timeBudgetReachedLocked(now time.Time) bool {
	if g.timeBudget <= 0 || g.startedAt.IsZero() || now.Before(g.startedAt) {
		return false
	}
	const maxDurationSeconds = int64((1<<63 - 1) / int64(time.Second))
	if g.timeBudget > maxDurationSeconds {
		return false
	}
	return now.Sub(g.startedAt) >= time.Duration(g.timeBudget)*time.Second
}

func (g *goalMachine) budgetReasonLocked(now time.Time, includeTurns bool) string {
	if includeTurns && g.turnBudget > 0 && g.turns >= g.turnBudget {
		return "goal turn budget reached"
	}
	if g.tokenBudget > 0 && g.tokensUsed >= g.tokenBudget {
		return "goal token budget reached"
	}
	if g.timeBudgetReachedLocked(now) {
		return "goal time budget reached"
	}
	return ""
}

func (g *goalMachine) markBudgetLimitedLocked(reason string, now time.Time) {
	g.status = GoalStatusBudgetLimited
	g.block = reason
	g.interceptMsg = ""
	g.intercepts = 0
	g.selfCheckDone = false
	g.idleTurns = 0
	g.terminalAt = now
}

func (g *goalMachine) recordBlockedLocked(rawReason string, now time.Time) (string, bool) {
	reason := cleanGoalBlockReason(rawReason)
	if reason == "" {
		reason = "blocked"
	}
	if sameGoalBlock(g.block, reason) {
		g.blocks++
	} else {
		g.blocks = 1
		g.block = reason
	}
	if g.blocks < 3 {
		return reason, false
	}
	g.status = GoalStatusBlocked
	g.idleTurns = 0
	g.terminalAt = now
	return reason, true
}

// checkBudget is the pre-provider gate. It atomically refreshes elapsed time
// and transitions a running goal before another orchestration round can start.
func (g *goalMachine) checkBudget(now time.Time) goalBudgetResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := goalBudgetResult{allowed: true}
	if g.status == GoalStatusBudgetLimited && strings.TrimSpace(g.objective) != "" {
		result.allowed = false
		return result
	}
	if strings.TrimSpace(g.goal) == "" || g.status != GoalStatusRunning {
		return result
	}
	changed := g.updateElapsedLocked(now)
	if reason := g.budgetReasonLocked(now, true); reason != "" {
		g.markBudgetLimitedLocked(reason, now)
		result.allowed = false
		result.notice = reason
		changed = true
	}
	if changed {
		result.path, result.data, result.ok = g.buildStateLocked(g.todos)
	}
	return result
}

// recordUsage accounts one successfully completed root-executor provider round.
// It never calls the executor and returns pre-marshaled state for off-lock I/O.
func (g *goalMachine) recordUsage(tokens int64, now time.Time) goalBudgetResult {
	return g.recordUsageForGeneration(tokens, now, 0, false)
}

func (g *goalMachine) recordUsageForGeneration(tokens int64, now time.Time, generation uint64, activeRound bool) goalBudgetResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := goalBudgetResult{allowed: true}
	if generation != 0 && generation != g.generation {
		return result
	}
	if g.status == GoalStatusRunning {
		if strings.TrimSpace(g.goal) == "" {
			return result
		}
	} else if !activeRound || strings.TrimSpace(g.objective) == "" {
		return result
	}
	changed := g.updateElapsedLocked(now)
	if tokens > 0 {
		const maxInt64 = int64(1<<63 - 1)
		if g.tokensUsed > maxInt64-tokens {
			g.tokensUsed = maxInt64
		} else {
			g.tokensUsed += tokens
		}
		changed = true
	}
	// A provider reports usage before the orchestrator can parse the final
	// assistant marker. Defer the active unit's budget transition so a valid
	// completion has the same precedence as completion on the last turn.
	if g.status == GoalStatusRunning && !activeRound {
		if reason := g.budgetReasonLocked(now, false); reason != "" {
			g.markBudgetLimitedLocked(reason, now)
			result.allowed = false
			result.notice = reason
			changed = true
		}
	}
	if changed {
		result.path, result.data, result.ok = g.buildStateLocked(g.todos)
	}
	return result
}

// interrupt records a transient provider/context failure without terminating
// the goal. A later turn can resume the same generation and budget counters.
func (g *goalMachine) interrupt(lastError string, now time.Time, todos []evidence.TodoItem) (string, []byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(g.goal) == "" || g.status != GoalStatusRunning {
		return "", nil, false
	}
	g.updateElapsedLocked(now)
	g.lastError = strings.TrimSpace(lastError)
	g.interruptedAt = now
	return g.buildStateLocked(todos)
}

// stop transitions a running goal to the given terminal status and clears the
// transient intercept/idle bookkeeping.
func (g *goalMachine) stop(status string, todos []evidence.TodoItem) (string, []byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(g.goal) != "" && g.status == GoalStatusRunning {
		now := time.Now().UTC()
		g.updateElapsedLocked(now)
		g.status = status
		g.terminalAt = now
	}
	g.interceptMsg = ""
	g.intercepts = 0
	g.selfCheckDone = false
	g.idleTurns = 0
	return g.buildStateLocked(todos)
}

// takeIntercept consumes a pending continuation-turn override, if any.
func (g *goalMachine) takeIntercept() (string, bool) {
	g.mu.Lock()
	if g.interceptMsg == "" {
		g.mu.Unlock()
		return "", false
	}
	msg := g.interceptMsg
	g.interceptMsg = ""
	path, data, ok := g.buildStateLocked(g.todos)
	g.mu.Unlock()
	if ok {
		g.writeState(path, data)
	}
	return msg, true
}

// advance runs one continuation step of the goal FSM from already-gathered
// inputs. It mutates the machine, decides whether to keep looping, and builds
// the state to persist when the goal reached a terminal/notice point.
func (g *goalMachine) advance(in goalAdvanceInput) goalAdvanceResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	if in.generation != g.generation {
		return goalAdvanceResult{cont: false}
	}
	if strings.TrimSpace(g.goal) == "" || g.status != GoalStatusRunning {
		return goalAdvanceResult{cont: false}
	}
	now := time.Now().UTC()
	g.updateElapsedLocked(now)
	g.turns++
	var notice string
	var override bool
	var controlSignal evidence.FailureSignal
	switch in.status {
	case GoalStatusComplete:
		g.blocks = 0
		g.block = ""
		if incomplete := formatIncompleteTodos(in.todos, in.readiness); len(incomplete) > 0 {
			if g.strict || g.intercepts == 0 {
				g.intercepts++
				controlSignal = evidence.FailureSignal{
					GoalAcceptanceLoop: g.intercepts,
					DifficultDecision:  true,
					DecisionSummary:    "goal completion was intercepted by readiness checks",
				}
				g.interceptMsg = incomplete
				break
			}
			override = true
			controlSignal = evidence.FailureSignal{
				GoalAcceptanceLoop: g.intercepts,
				DifficultDecision:  true,
				DecisionSummary:    "goal completion override accepted",
			}
		}
		// Todos are all done — in strict mode run self-check before final
		// completion. Non-strict mode completes immediately.
		if g.strict && !g.selfCheckDone {
			g.selfCheckDone = true
			g.interceptMsg = goalSelfCheckTurn
			break
		}
		// Self-check passed — complete the goal.
		g.intercepts = 0
		g.selfCheckDone = false
		g.idleTurns = 0
		g.goal = ""
		g.status = GoalStatusComplete
		g.terminalAt = now
		g.interceptMsg = ""
		notice = goalCompleteNotice
	case GoalStatusBlocked:
		g.intercepts = 0
		g.interceptMsg = ""
		g.selfCheckDone = false
		if reason, terminal := g.recordBlockedLocked(in.reason, now); terminal {
			notice = "goal blocked: " + reason
		}
	default:
		g.blocks = 0
		g.block = ""
		g.intercepts = 0
		g.selfCheckDone = false
	}
	// Idle detection: if the agent went multiple turns without any tool calls,
	// inject a reminder to make progress (unless the goal is already completing
	// or hitting the auto-turn limit).
	if notice == "" && g.interceptMsg == "" {
		if in.toolCalled {
			g.idleTurns = 0
		} else {
			g.idleTurns++
			if g.idleTurns >= maxGoalIdleTurns {
				g.idleTurns = 0
				g.interceptMsg = "No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>]."
			}
		}
	}
	if notice == "" && g.status == GoalStatusRunning {
		if reason := g.budgetReasonLocked(now, true); reason != "" {
			g.markBudgetLimitedLocked(reason, now)
			notice = reason
		}
	}
	res := goalAdvanceResult{notice: notice, cont: notice == "", override: override}
	if res.cont || res.override {
		res.controlSignal = controlSignal
		if in.status == GoalStatusBlocked {
			res.controlSignal.GoalAcceptanceLoop = g.blocks
			res.controlSignal.DifficultDecision = true
			res.controlSignal.DecisionSummary = "goal blocked: " + cleanGoalBlockReason(in.reason)
		}
	}
	res.path, res.data, res.ok = g.buildStateLocked(in.todos)
	return res
}

// buildStateLocked marshals the current goal state for persistence. The caller
// holds mu; this only reads in-memory state, never touching disk. Returns ok=false
// when persistence is disabled (no state path). The matching writeState does the
// disk write OFF mu so the per-turn save can't stall a status poll.
func (g *goalMachine) buildStateLocked(todos []evidence.TodoItem) (path string, data []byte, ok bool) {
	g.todos = append([]evidence.TodoItem(nil), todos...)
	g.revision++
	g.updatedAt = time.Now().UTC()
	state := g.snapshotLocked()
	b, err := json.Marshal(state)
	if err != nil {
		slog.Warn("controller: marshal goal state", "err", err)
		return "", nil, false
	}
	if g.statePath == "" {
		return "", nil, false
	}
	return g.statePath, b, true
}

// writeState persists pre-marshaled goal-state bytes to disk, OFF mu and
// serialized by writeMu so concurrent saves don't interleave or land out of
// order. Best-effort: failures are logged, not surfaced.
func (g *goalMachine) writeState(path string, data []byte) {
	if path == "" || data == nil {
		return
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	pathLock := goalPathLock(path)
	pathLock.Lock()
	defer pathLock.Unlock()

	var incoming GoalSnapshot
	if err := json.Unmarshal(data, &incoming); err != nil {
		slog.Warn("controller: parse outgoing goal state", "err", err)
		return
	}
	if current, found, err := readGoalSnapshot(path); err == nil && found {
		newerGeneration := current.Generation > incoming.Generation
		staleRevision := current.Generation == incoming.Generation && current.Revision >= incoming.Revision
		if newerGeneration || staleRevision {
			g.reconcileWriteConflict(path, incoming, current)
			return
		}
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o644); err != nil {
		slog.Warn("controller: write goal state", "err", err)
	}
}

func (g *goalMachine) reconcileWriteConflict(path string, incoming, current GoalSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.statePath != path || g.generation != incoming.Generation {
		return
	}
	g.applySnapshotLocked(current, path)
}

// terminalTodosFromState reads the persisted goal-state sidecar and returns its
// todo snapshot only after the goal has reached a terminal state. Running goal
// state is not refreshed on every todo_write, so its todos may be older than the
// transcript rebuilt by Agent.SetSession.
func (g *goalMachine) terminalTodosFromState(sessionPath string) ([]evidence.TodoItem, bool) {
	state, found, err := readGoalSnapshot(goalStatePath(sessionPath))
	if err != nil {
		slog.Warn("controller: read goal state", "err", err)
		return nil, false
	}
	if !found {
		return nil, false
	}
	switch state.Status {
	case GoalStatusComplete, GoalStatusBlocked, GoalStatusStopped, GoalStatusBudgetLimited:
	default:
		return nil, false
	}
	if len(state.Todos) == 0 {
		return nil, false
	}
	return append([]evidence.TodoItem(nil), state.Todos...), true
}

// formatIncompleteTodos renders the reminder shown when [goal:complete] arrives
// while the executor's canonical todos or project-readiness checks aren't done.
// Returns empty when nothing is blocking. Pure: the caller gathers todos and the
// readiness reason from the executor off the goal lock.
func formatIncompleteTodos(todos []evidence.TodoItem, readiness string) string {
	var parts []string
	if len(todos) > 0 {
		if incomplete := evidence.IncompleteTodos(todos); len(incomplete) > 0 {
			var b strings.Builder
			b.WriteString("the following tasks are still incomplete:")
			for _, t := range incomplete {
				fmt.Fprintf(&b, "\n  - %s (%s)", t.Content, t.Status)
			}
			parts = append(parts, b.String())
		}
	}
	if readiness != "" {
		parts = append(parts, readiness)
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Goal signaled complete but issues remain:\n")
	for _, p := range parts {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("Fix or use todo_write/complete_step to mark done, then [goal:complete] again.")
	return b.String()
}

func parseGoalStatusMarker(text string) (status, reason string, ok bool) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch lower {
		case "[goal:complete]":
			return GoalStatusComplete, "", true
		case "[goal:continue]":
			return GoalStatusRunning, "", true
		}
		const blockedPrefix = "[goal:blocked:"
		if strings.HasPrefix(lower, blockedPrefix) && strings.HasSuffix(line, "]") {
			return GoalStatusBlocked, strings.TrimSpace(line[len(blockedPrefix) : len(line)-1]), true
		}
		return "", "", false
	}
	return "", "", false
}

func sameGoalBlock(a, b string) bool {
	return normalizeGoalBlockReason(a) == normalizeGoalBlockReason(b)
}

func cleanGoalBlockReason(reason string) string {
	return strings.Trim(strings.TrimSpace(reason), " \t\r\n:：,，.。;；!！?？-—_[]()（）")
}

func normalizeGoalBlockReason(reason string) string {
	reason = strings.ToLower(cleanGoalBlockReason(reason))
	var b strings.Builder
	lastSpace := true
	for _, r := range reason {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// ShortGoalForNotice collapses whitespace and truncates a goal for one-line UI.
func ShortGoalForNotice(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	runes := []rune(goal)
	const max = 160
	if len(runes) <= max {
		return goal
	}
	return string(runes[:max]) + "..."
}

// goalTodos snapshots the executor's canonical todos for goal-state persistence.
func (c *Controller) goalTodos() []evidence.TodoItem {
	if c.executor == nil {
		return nil
	}
	return c.executor.CanonicalTodoState()
}

// persistGoalState writes a freshly built goal state to disk, off c.mu. The
// executor guard preserves the original behavior of skipping persistence when
// no executor is attached.
func (c *Controller) persistGoalState(path string, data []byte, ok bool) {
	if !ok || c.executor == nil {
		return
	}
	c.goals.writeState(path, data)
}

func (c *Controller) restoreTerminalGoalTodos(sessionPath string) {
	if c.executor == nil {
		return
	}
	todos, ok := c.goals.terminalTodosFromState(sessionPath)
	if !ok {
		return
	}
	c.executor.ReplaceTodoState(todos)
}
