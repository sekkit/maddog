package skillopt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"maddog/internal/skill"
)

func TestEngineCompletesPairedValidationAndHeldOutTest(t *testing.T) {
	store := NewJSONRunStore(t.TempDir())
	rollout := &scriptRollout{}
	proposer := &replaceBodyProposer{body: "good instructions"}
	engine := NewEngine(EngineOptions{Store: store, Rollout: rollout, Proposer: proposer, Now: fixedClock()})
	run, err := engine.Start(context.Background(), Request{
		RunID: "paired-run", Dataset: testDataset(), Skill: testSkill("bad instructions"),
		Config: Config{MaxRounds: 1, TrainBatchSize: 1, Seed: 9, MinDelta: 0.1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusCompleted || len(run.Rounds) != 1 || !run.Rounds[0].Completed || run.Rounds[0].Decision == nil || !run.Rounds[0].Decision.Accepted {
		t.Fatalf("run did not accept improved candidate: %+v", run)
	}
	if run.BestRevisionID == run.BaselineRevisionID || run.CurrentRevisionID != run.BestRevisionID {
		t.Fatalf("revision pointers not advanced: baseline=%s current=%s best=%s", run.BaselineRevisionID, run.CurrentRevisionID, run.BestRevisionID)
	}
	best, ok := run.Revision(run.BestRevisionID)
	if !ok || best.Artifact.Skill.Body != "good instructions" {
		t.Fatalf("best revision = %+v, %v", best, ok)
	}
	if !run.Test.Completed || run.Test.RevisionID != run.BestRevisionID || len(run.Test.CaseIDs) != 1 {
		t.Fatalf("held-out test not completed on best revision: %+v", run.Test)
	}
	for _, record := range run.Evaluations {
		if record.Phase == PhaseTest && record.RevisionID != run.BestRevisionID {
			t.Fatalf("test leakage/evaluation on non-best revision: %+v", record)
		}
	}
	if proposer.calls != 1 || rollout.calls != 8 || run.Usage.Calls != 9 {
		t.Fatalf("calls proposer=%d rollout=%d usage=%d, want 1/8/9", proposer.calls, rollout.calls, run.Usage.Calls)
	}
	if run.Usage.InputTokens != 9 || run.Usage.OutputTokens != 9 {
		t.Fatalf("usage not aggregated across every operation: %+v", run.Usage)
	}
}

func TestEngineRejectsCapabilityMutation(t *testing.T) {
	store := NewJSONRunStore(t.TempDir())
	proposer := &replaceBodyProposer{body: "good instructions", mutateTools: true}
	engine := NewEngine(EngineOptions{Store: store, Rollout: &scriptRollout{}, Proposer: proposer})
	run, err := engine.Start(context.Background(), Request{
		RunID: "frozen-fields", Dataset: testDataset(), Skill: testSkill("bad instructions"),
		Config: Config{MaxRounds: 1, TrainBatchSize: 1},
	})
	if !errors.Is(err, ErrCapabilityMutation) {
		t.Fatalf("Start error = %v, want ErrCapabilityMutation", err)
	}
	if run == nil || run.Status != StatusPaused {
		t.Fatalf("run = %+v, want durable paused checkpoint", run)
	}
	loaded, loadErr := store.Load(context.Background(), "frozen-fields")
	if loadErr != nil || loaded.Status != StatusPaused {
		t.Fatalf("checkpoint = %+v, %v", loaded, loadErr)
	}
}

func TestEngineStopsAtBudgetAndCanBeCanceled(t *testing.T) {
	t.Run("budget", func(t *testing.T) {
		store := NewJSONRunStore(t.TempDir())
		engine := NewEngine(EngineOptions{Store: store, Rollout: &scriptRollout{}, Proposer: &replaceBodyProposer{body: "good"}})
		run, err := engine.Start(context.Background(), Request{
			RunID: "budget", Dataset: testDataset(), Skill: testSkill("bad"),
			Config: Config{MaxRounds: 1, TrainBatchSize: 1, Budget: Budget{MaxCalls: 2}},
		})
		if !errors.Is(err, ErrBudgetExceeded) || run == nil || run.Status != StatusBudgetExhausted || run.Usage.Calls != 2 {
			t.Fatalf("budget result run=%+v err=%v", run, err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store := NewJSONRunStore(t.TempDir())
		rollout := &cancelingRollout{store: store, runID: "cancel"}
		engine := NewEngine(EngineOptions{Store: store, Rollout: rollout, Proposer: &replaceBodyProposer{body: "good"}})
		run, err := engine.Start(context.Background(), Request{
			RunID: "cancel", Dataset: testDataset(), Skill: testSkill("bad"),
			Config: Config{MaxRounds: 2, TrainBatchSize: 1},
		})
		if !errors.Is(err, ErrCanceled) || run == nil || run.Status != StatusCanceled || !run.CancelRequested {
			t.Fatalf("cancel result run=%+v err=%v", run, err)
		}
		if rollout.calls != 1 {
			t.Fatalf("rollout calls after cancel = %d, want 1", rollout.calls)
		}
	})
}

func TestEnginePersistsProposalBeforeValidationAndReusesItOnResume(t *testing.T) {
	store := NewJSONRunStore(t.TempDir())
	rollout := &pauseValidationRollout{}
	proposer := &replaceBodyProposer{body: "good instructions"}
	engine := NewEngine(EngineOptions{Store: store, Rollout: rollout, Proposer: proposer})
	run, err := engine.Start(context.Background(), Request{
		RunID: "resume-proposal", Dataset: testDataset(), Skill: testSkill("bad instructions"),
		Config: Config{MaxRounds: 1, TrainBatchSize: 1},
	})
	if !errors.Is(err, errValidationPause) || run == nil || run.Status != StatusPaused {
		t.Fatalf("first run = %+v, err=%v, want paused validation checkpoint", run, err)
	}
	if len(run.Rounds) != 1 || run.Rounds[0].Proposal == nil || len(run.Revisions) != 2 {
		t.Fatalf("paused round did not persist proposal/revision: %+v", run)
	}
	if proposer.calls != 1 {
		t.Fatalf("proposer calls before resume = %d, want 1", proposer.calls)
	}

	run, err = engine.Resume(context.Background(), "resume-proposal")
	if err != nil || run.Status != StatusCompleted {
		t.Fatalf("resume = %+v, err=%v", run, err)
	}
	if proposer.calls != 1 {
		t.Fatalf("proposer calls after resume = %d, want persisted proposal reuse", proposer.calls)
	}
	var seeds map[EvaluationRole]int64
	seeds = map[EvaluationRole]int64{}
	for _, record := range run.Evaluations {
		if record.Phase == PhaseValidation && record.CaseID == "val-a" {
			seeds[record.Role] = record.Seed
		}
	}
	if len(seeds) != 3 || seeds[RoleBaseline] != seeds[RoleCurrent] || seeds[RoleCurrent] != seeds[RoleCandidate] {
		t.Fatalf("paired validation seeds = %+v, want one seed for all roles", seeds)
	}
}

func TestEngineResumeRejectsTamperedDatasetCheckpoint(t *testing.T) {
	store := NewJSONRunStore(t.TempDir())
	engine := NewEngine(EngineOptions{Store: store, Rollout: &pauseValidationRollout{}, Proposer: &replaceBodyProposer{body: "good instructions"}})
	run, err := engine.Start(context.Background(), Request{
		RunID: "tampered-dataset", Dataset: testDataset(), Skill: testSkill("bad instructions"),
		Config: Config{MaxRounds: 1, TrainBatchSize: 1},
	})
	if !errors.Is(err, errValidationPause) || run == nil {
		t.Fatalf("create paused checkpoint = %+v, %v", run, err)
	}
	run.Dataset.Train[0].Input = "tampered input"
	if err := store.Save(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Resume(context.Background(), run.ID); !errors.Is(err, ErrInvalidDataset) || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("resume tampered dataset error = %v", err)
	}
}

func TestLoadDatasetRejectsSplitOverlapAndLoadsTOML(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	data, _ := json.Marshal(Dataset{
		ID: "bad", Train: []Case{{ID: "same", Input: "a"}},
		Validation: []Case{{ID: "same", Input: "b"}}, Test: []Case{{ID: "test", Input: "c"}},
	})
	if err := os.WriteFile(bad, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDataset(bad); !errors.Is(err, ErrInvalidDataset) || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("overlap error = %v", err)
	}

	manifest := filepath.Join(dir, "dataset.toml")
	if err := os.WriteFile(manifest, []byte(`
id = "fixture"
[[train]]
id = "train-1"
input = "train"
expected = "ok"
[train.metadata]
task_id = "train-1"
[[validation]]
id = "val-1"
input = "validate"
[[test]]
id = "test-1"
input = "test"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataset, err := LoadDataset(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if dataset.ID != "fixture" || len(dataset.Train) != 1 || string(dataset.Train[0].Expected) != `"ok"` || dataset.Train[0].Metadata["task_id"] != "train-1" {
		t.Fatalf("TOML dataset = %+v", dataset)
	}
}

func TestJSONRunStoreCleanupKeepsActiveAndRollbackCapableRuns(t *testing.T) {
	store := NewJSONRunStore(t.TempDir())
	now := time.Now().UTC()
	makeRun := func(id string, status RunStatus) *Run {
		return &Run{SchemaVersion: SchemaVersion, ID: id, Status: status, CreatedAt: now, UpdatedAt: now}
	}
	completed := makeRun("completed", StatusCompleted)
	active := makeRun("active", StatusRunning)
	promoted := makeRun("promoted", StatusCompleted)
	promoted.Promotion = &PromotionRecord{PromotedAt: now, PromotedHash: strings.Repeat("a", 64)}
	for _, run := range []*Run{completed, active, promoted} {
		if err := store.Create(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.Cleanup(context.Background(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "completed" {
		t.Fatalf("removed = %v, want only completed", removed)
	}
	if _, err := store.Load(context.Background(), "active"); err != nil {
		t.Fatalf("active run removed: %v", err)
	}
	if _, err := store.Load(context.Background(), "promoted"); err != nil {
		t.Fatalf("rollback-capable run removed: %v", err)
	}
}

type scriptRollout struct {
	mu    sync.Mutex
	calls int
}

func (r *scriptRollout) Evaluate(_ context.Context, req RolloutRequest) (Result, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	good := strings.Contains(req.Skill.Body, "good")
	return Result{Hard: good, Soft: map[bool]float64{true: 0.9, false: 0.2}[good], Cost: Cost{InputTokens: 1, OutputTokens: 1}, ModelRef: "fixture"}, nil
}

type cancelingRollout struct {
	store *JSONRunStore
	runID string
	calls int
}

var errValidationPause = errors.New("validation paused fixture")

type pauseValidationRollout struct {
	paused bool
}

func (r *pauseValidationRollout) Evaluate(_ context.Context, req RolloutRequest) (Result, error) {
	if req.Phase == PhaseValidation && req.Role == RoleBaseline && !r.paused {
		r.paused = true
		return Result{}, errValidationPause
	}
	good := strings.Contains(req.Skill.Body, "good")
	return Result{Hard: good, Soft: map[bool]float64{true: 0.9, false: 0.2}[good], Cost: Cost{InputTokens: 1, OutputTokens: 1}, ModelRef: "fixture"}, nil
}

func (r *cancelingRollout) Evaluate(ctx context.Context, req RolloutRequest) (Result, error) {
	r.calls++
	if r.calls == 1 {
		if err := r.store.Cancel(ctx, r.runID); err != nil {
			return Result{}, err
		}
	}
	return Result{Soft: 0.2, Cost: Cost{InputTokens: 1, OutputTokens: 1}}, nil
}

type replaceBodyProposer struct {
	body        string
	mutateTools bool
	calls       int
}

func (p *replaceBodyProposer) Propose(_ context.Context, req ProposalRequest) (Proposal, error) {
	p.calls++
	candidate := req.Base.Artifact.Skill
	candidate.Body = p.body
	if p.mutateTools {
		candidate.AllowedTools = append(candidate.AllowedTools, "write_file")
	}
	return Proposal{
		Candidate: candidate,
		Edits:     []BodyEdit{{Start: 0, End: len(req.Base.Artifact.Skill.Body), Replacement: p.body}},
		Cost:      Cost{InputTokens: 1, OutputTokens: 1}, ModelRef: "fixture-proposer",
	}, nil
}

func testDataset() Dataset {
	return Dataset{
		ID:         "fixture",
		Train:      []Case{{ID: "train-a", Input: "train a"}, {ID: "train-b", Input: "train b"}},
		Validation: []Case{{ID: "val-a", Input: "validate a"}, {ID: "val-b", Input: "validate b"}},
		Test:       []Case{{ID: "test-a", Input: "test a"}},
	}
}

func testSkill(body string) skill.Skill {
	return skill.Skill{Name: "fixture-skill", Description: "fixture", Body: body, Scope: skill.ScopeProject, Path: "fixture/SKILL.md", RunAs: skill.RunInline, AllowedTools: []string{"read_file"}}
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 7, 13, 1, 2, 3, 0, time.UTC)
	return func() time.Time {
		now = now.Add(time.Millisecond)
		return now
	}
}
