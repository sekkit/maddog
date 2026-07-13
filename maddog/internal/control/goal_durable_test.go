package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/event"
	"maddog/internal/evidence"
)

func newGoalPersistenceController(path string) *Controller {
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	return New(Options{
		Executor:    exec,
		SessionDir:  filepath.Dir(path),
		SessionPath: path,
		Label:       "goal-persistence-test",
	})
}

func readPersistedGoalSnapshot(t *testing.T, sessionPath string) GoalSnapshot {
	t.Helper()
	data, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatalf("read goal snapshot: %v", err)
	}
	var state GoalSnapshot
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode goal snapshot: %v", err)
	}
	return state
}

func TestGoalSnapshotRestoresRunningFSM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	c := newGoalPersistenceController(path)
	c.SetGoalWithResearchMode("ship durable goals", GoalResearchOn)
	c.GoalStrict(true)

	todos := []evidence.TodoItem{{Content: "persist state", Status: "in_progress"}}
	res := c.goals.advance(goalAdvanceInput{
		generation: c.GoalSnapshot().Generation,
		status:     GoalStatusRunning,
		toolCalled: true,
		todos:      todos,
	})
	c.persistGoalState(res.path, res.data, res.ok)
	want := c.GoalSnapshot()
	if want.ID == "" || want.Generation == 0 || want.Revision < 3 {
		t.Fatalf("initial snapshot identity = %+v", want)
	}
	if want.CreatedAt.IsZero() || want.StartedAt.IsZero() || want.UpdatedAt.IsZero() {
		t.Fatalf("initial snapshot timestamps = %+v", want)
	}

	resumed := newGoalPersistenceController(filepath.Join(dir, "placeholder.jsonl"))
	resumed.Resume(agent.NewSession("sys"), path)
	got := resumed.GoalSnapshot()
	if got.ID != want.ID || got.Objective != want.Objective || got.Goal != want.Goal {
		t.Fatalf("restored identity = %+v, want %+v", got, want)
	}
	if got.Status != GoalStatusRunning || got.Mode != GoalModeAutonomous || got.ResearchMode != GoalResearchOn || !got.Strict {
		t.Fatalf("restored mode = %+v", got)
	}
	if got.Turns != 1 || got.Generation != want.Generation || got.Revision != want.Revision {
		t.Fatalf("restored counters = %+v, want %+v", got, want)
	}
	if len(got.Todos) != 1 || got.Todos[0].Content != "persist state" {
		t.Fatalf("restored todos = %+v", got.Todos)
	}
	got.Todos[0].Content = "mutated copy"
	if resumed.GoalSnapshot().Todos[0].Content != "persist state" {
		t.Fatal("GoalSnapshot returned an aliased todo slice")
	}
}

func TestGoalSnapshotRetainsCompletedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	c := newGoalPersistenceController(path)
	c.SetGoal("finish the release")
	started := c.GoalSnapshot()

	res := c.goals.advance(goalAdvanceInput{generation: started.Generation, status: GoalStatusComplete, toolCalled: true})
	c.persistGoalState(res.path, res.data, res.ok)
	if got := c.Goal(); got != "" {
		t.Fatalf("legacy Goal() = %q after completion, want empty", got)
	}
	completed := c.GoalSnapshot()
	if completed.ID != started.ID || completed.Objective != "finish the release" {
		t.Fatalf("completed identity = %+v, started %+v", completed, started)
	}
	if completed.Status != GoalStatusComplete || completed.TerminalAt.IsZero() {
		t.Fatalf("completed terminal state = %+v", completed)
	}

	resumed := newGoalPersistenceController(filepath.Join(t.TempDir(), "other.jsonl"))
	resumed.Resume(agent.NewSession("sys"), path)
	restored := resumed.GoalSnapshot()
	if restored.ID != started.ID || restored.Objective != started.Objective || restored.Status != GoalStatusComplete {
		t.Fatalf("restored completed state = %+v", restored)
	}
}

func TestGoalSnapshotRejectsStaleRevisionAndGeneration(t *testing.T) {
	path := goalStatePath(filepath.Join(t.TempDir(), "session.jsonl"))
	var first goalMachine
	first.bindStatePath(path, false, false)
	oldPath, oldData, ok := first.set("first goal", GoalResearchAuto, nil)
	if !ok {
		t.Fatal("first goal did not produce persisted state")
	}
	newPath, newData, ok := first.setStrict(true, nil)
	if !ok {
		t.Fatal("strict transition did not produce persisted state")
	}
	first.writeState(newPath, newData)
	first.writeState(oldPath, oldData)
	current, found, err := readGoalSnapshot(path)
	if err != nil || !found {
		t.Fatalf("read current snapshot: found=%v err=%v", found, err)
	}
	if !current.Strict || current.Revision < 2 {
		t.Fatalf("stale revision overwrote newer state: %+v", current)
	}

	var second goalMachine
	if !second.bindStatePath(path, true, false) {
		t.Fatal("second machine did not load first generation")
	}
	replacementPath, replacementData, ok := second.set("replacement goal", GoalResearchOff, nil)
	if !ok {
		t.Fatal("replacement did not produce persisted state")
	}
	second.writeState(replacementPath, replacementData)
	stalePath, staleData, ok := first.setStrict(false, nil)
	if !ok {
		t.Fatal("old generation did not produce stale state")
	}
	first.writeState(stalePath, staleData)
	current, found, err = readGoalSnapshot(path)
	if err != nil || !found {
		t.Fatalf("read replacement snapshot: found=%v err=%v", found, err)
	}
	if current.Objective != "replacement goal" || current.Generation <= 1 {
		t.Fatalf("stale generation overwrote replacement: %+v", current)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".atomic-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("atomic temp files after write = %v, err=%v", matches, err)
	}
}

func TestGoalSnapshotRejectsConcurrentEqualRevision(t *testing.T) {
	path := goalStatePath(filepath.Join(t.TempDir(), "session.jsonl"))
	var seed goalMachine
	seed.bindStatePath(path, false, false)
	seedPath, seedData, ok := seed.set("shared goal", GoalResearchAuto, nil)
	if !ok {
		t.Fatal("seed goal did not produce persisted state")
	}
	seed.writeState(seedPath, seedData)

	var first, second goalMachine
	if !first.bindStatePath(path, true, false) || !second.bindStatePath(path, true, false) {
		t.Fatal("concurrent writers did not load the same goal snapshot")
	}
	generation := first.durableSnapshot().Generation
	completed := first.advance(goalAdvanceInput{generation: generation, status: GoalStatusComplete, toolCalled: true})
	time.Sleep(2 * time.Millisecond)
	blocked := second.advance(goalAdvanceInput{generation: generation, status: GoalStatusBlocked, reason: "needs access"})
	if first.durableSnapshot().Revision != second.durableSnapshot().Revision {
		t.Fatal("test setup did not create equal revisions")
	}

	first.writeState(completed.path, completed.data)
	second.writeState(blocked.path, blocked.data)
	got, found, err := readGoalSnapshot(path)
	if err != nil || !found {
		t.Fatalf("read winning snapshot: found=%v err=%v", found, err)
	}
	if got.Status != GoalStatusComplete {
		t.Fatalf("equal-revision writer overwrote first committed state: %+v", got)
	}

	retry := second.advance(goalAdvanceInput{generation: generation, status: GoalStatusRunning, toolCalled: true})
	second.writeState(retry.path, retry.data)
	got, found, err = readGoalSnapshot(path)
	if err != nil || !found {
		t.Fatalf("read snapshot after losing writer retry: found=%v err=%v", found, err)
	}
	if got.Status != GoalStatusComplete || second.durableSnapshot().Status != GoalStatusComplete {
		t.Fatalf("losing writer was not reconciled to committed state: disk=%+v local=%+v", got, second.durableSnapshot())
	}
}

func TestGoalBindingDoesNotLeakAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.jsonl")
	pathB := filepath.Join(dir, "b.jsonl")
	pathC := filepath.Join(dir, "c.jsonl")
	c := newGoalPersistenceController(pathA)
	c.SetGoal("session A goal")
	wantA := c.GoalSnapshot()

	if err := os.WriteFile(goalStatePath(pathB), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Resume(agent.NewSession("sys"), pathB)
	if got := c.GoalSnapshot(); got.Objective != "" || c.GoalStatus() != GoalStatusStopped {
		t.Fatalf("corrupt target inherited prior goal: %+v", got)
	}
	c.Resume(agent.NewSession("sys"), pathA)
	if got := c.GoalSnapshot(); got.ID != wantA.ID || got.Objective != wantA.Objective {
		t.Fatalf("session A did not restore independently: %+v", got)
	}
	c.SetSessionPath(pathC)
	if got := c.GoalSnapshot(); got.Objective != "" || c.GoalStatus() != GoalStatusStopped {
		t.Fatalf("fresh SetSessionPath inherited prior goal: %+v", got)
	}

	preboundPath := filepath.Join(dir, "prebound.jsonl")
	prebound := newGoalPersistenceController("")
	prebound.SetGoal("goal configured before path")
	prebound.SetSessionPath(preboundPath)
	if got := readPersistedGoalSnapshot(t, preboundPath); got.Objective != "goal configured before path" {
		t.Fatalf("initial path adoption lost goal: %+v", got)
	}
}

func TestGoalEveryAdvanceAndInterceptPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	c := newGoalPersistenceController(path)
	c.SetGoal("keep making progress")
	initial := readPersistedGoalSnapshot(t, path)

	generation := c.GoalSnapshot().Generation
	res := c.goals.advance(goalAdvanceInput{generation: generation, status: GoalStatusBlocked, reason: "needs access"})
	c.persistGoalState(res.path, res.data, res.ok)
	blockedOnce := readPersistedGoalSnapshot(t, path)
	if blockedOnce.Turns != 1 || blockedOnce.Blocks != 1 || blockedOnce.Revision <= initial.Revision {
		t.Fatalf("first blocked transition not persisted: %+v", blockedOnce)
	}

	res = c.goals.advance(goalAdvanceInput{generation: generation, status: GoalStatusRunning})
	c.persistGoalState(res.path, res.data, res.ok)
	idle := readPersistedGoalSnapshot(t, path)
	if idle.Turns != 2 || idle.InterceptMsg == "" || idle.Revision <= blockedOnce.Revision {
		t.Fatalf("continuation/idle transition not persisted: %+v", idle)
	}
	if msg, ok := c.goals.takeIntercept(); !ok || !strings.Contains(msg, "No tool calls") {
		t.Fatalf("takeIntercept = %q, %v", msg, ok)
	}
	consumed := readPersistedGoalSnapshot(t, path)
	if consumed.InterceptMsg != "" || consumed.Revision <= idle.Revision {
		t.Fatalf("intercept consumption not persisted: %+v", consumed)
	}
}

func TestGoalLegacySnapshotMigratesOnResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	legacy := []byte(`{"goal":"legacy goal","status":"running","researchMode":1,"turns":7,"blocks":2,"block":"needs input","strict":true}`)
	if err := os.WriteFile(goalStatePath(path), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	c := newGoalPersistenceController(filepath.Join(t.TempDir(), "placeholder.jsonl"))
	c.Resume(agent.NewSession("sys"), path)
	got := c.GoalSnapshot()
	if got.SchemaVersion != GoalSnapshotSchemaVersion || got.ID == "" || got.Objective != "legacy goal" {
		t.Fatalf("legacy identity migration = %+v", got)
	}
	if got.Status != GoalStatusRunning || got.Turns != 7 || got.Blocks != 2 || !got.Strict || got.ResearchMode != GoalResearchOn {
		t.Fatalf("legacy state migration = %+v", got)
	}
}

func TestClearSessionRemovesGoalSidecarAndResetsFSM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	c := newGoalPersistenceController(path)
	c.SetGoal("discard me")
	if err := c.ClearSession(); err != nil {
		t.Fatal(err)
	}
	if got := c.GoalSnapshot(); got.Objective != "" || c.GoalStatus() != GoalStatusStopped {
		t.Fatalf("ClearSession retained goal: %+v", got)
	}
	if _, err := os.Stat(goalStatePath(path)); !os.IsNotExist(err) {
		t.Fatalf("old goal sidecar remains after clear: %v", err)
	}
}
