package skillopt

// This file contains the orchestration loop. The engine intentionally knows
// nothing about a model provider or a benchmark implementation: callers adapt
// those to RolloutExecutor and Proposer. Keeping those boundaries explicit is
// important because optimization runs must be reproducible and resumable.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"maddog/internal/skill"
)

// Engine executes bounded optimization runs and persists every state change
// through Store. All collaborators are required; use a deterministic fixture
// implementation in tests when no model provider is available.
type Engine struct {
	Store RunStore
	// Rollout is the public name used by lifecycle callers. Executor is kept
	// as an alias for adapters written against the initial package draft.
	Rollout  RolloutExecutor
	Executor RolloutExecutor
	Proposer Proposer
	Gate     Gate
	Now      func() time.Time
}

// EngineOptions configures an optimization engine.
type EngineOptions struct {
	Store    RunStore
	Rollout  RolloutExecutor
	Executor RolloutExecutor
	Proposer Proposer
	Gate     Gate
	Now      func() time.Time
}

// NewEngine constructs an optimization engine. It accepts EngineOptions and,
// for compatibility with the original draft API, (store, rollout, proposer)
// positional arguments.
func NewEngine(options any, args ...any) *Engine {
	var config EngineOptions
	switch value := options.(type) {
	case EngineOptions:
		config = value
	case RunStore:
		config.Store = value
		if len(args) > 0 {
			config.Rollout, _ = args[0].(RolloutExecutor)
		}
		if len(args) > 1 {
			config.Proposer, _ = args[1].(Proposer)
		}
	}
	if config.Rollout == nil {
		config.Rollout = config.Executor
	}
	if config.Executor == nil {
		config.Executor = config.Rollout
	}
	if config.Gate == nil {
		config.Gate = StrictGate{}
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Engine{Store: config.Store, Rollout: config.Rollout, Executor: config.Executor, Proposer: config.Proposer, Gate: config.Gate, Now: config.Now}
}

// New is a short alias retained for callers that use constructors named after
// their package.
func New(options any, args ...any) *Engine {
	return NewEngine(options, args...)
}

// DatasetDigest returns the canonical SHA-256 digest stored in a checkpoint.
func DatasetDigest(dataset Dataset) (string, error) {
	if err := ValidateDataset(dataset); err != nil {
		return "", err
	}
	return datasetDigest(dataset)
}

// ValidateConfig rejects configurations that could result in an unbounded or
// non-deterministic run. Zero-valued configs are accepted by applying defaults
// in NormalizeConfig.
func ValidateConfig(config Config) error {
	if config.MaxRounds < 0 || config.TrainBatchSize < 0 || config.MaxConcurrency < 0 {
		return fmt.Errorf("%w: round, batch, and concurrency values cannot be negative", ErrInvalidConfig)
	}
	if config.EditLimits.MaxEdits < 0 || config.EditLimits.MaxChangedBytes < 0 || config.EditLimits.MaxBodyBytes < 0 {
		return fmt.Errorf("%w: edit limits cannot be negative", ErrInvalidConfig)
	}
	if config.Budget.MaxCalls < 0 || config.Budget.MaxInputTokens < 0 || config.Budget.MaxOutputTokens < 0 || config.Budget.MaxAmount < 0 {
		return fmt.Errorf("%w: budget values cannot be negative", ErrInvalidConfig)
	}
	if config.MinDelta < 0 || config.Deadband < 0 {
		return fmt.Errorf("%w: gate thresholds cannot be negative", ErrInvalidConfig)
	}
	return nil
}

// NormalizeConfig fills omitted values while preserving explicit zero gate
// thresholds and budgets (zero budget fields mean unlimited).
func NormalizeConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.MaxRounds == 0 {
		config.MaxRounds = defaults.MaxRounds
	}
	if config.TrainBatchSize == 0 {
		config.TrainBatchSize = defaults.TrainBatchSize
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = defaults.MaxConcurrency
	}
	if config.EditLimits.MaxEdits == 0 {
		config.EditLimits.MaxEdits = defaults.EditLimits.MaxEdits
	}
	if config.EditLimits.MaxChangedBytes == 0 {
		config.EditLimits.MaxChangedBytes = defaults.EditLimits.MaxChangedBytes
	}
	if config.EditLimits.MaxBodyBytes == 0 {
		config.EditLimits.MaxBodyBytes = defaults.EditLimits.MaxBodyBytes
	}
	// A zero seed is valid and deterministic; use the default only when the
	// entire config was omitted (the common JSON/TOML case).
	if config.Seed == 0 {
		config.Seed = defaults.Seed
	}
	if config.MinDelta == 0 && config.Deadband == 0 {
		config.MinDelta = defaults.MinDelta
		config.Deadband = defaults.Deadband
	}
	return config
}

// Start creates and executes a new run. The returned checkpoint is always the
// durable state reached before the method returned, including on cancellation
// or a provider error.
func (e *Engine) Start(ctx context.Context, request Request) (*Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e == nil || e.Store == nil || e.rollout() == nil || e.Proposer == nil {
		return nil, fmt.Errorf("%w: engine requires store, executor, and proposer", ErrInvalidConfig)
	}
	if strings.TrimSpace(request.RunID) == "" {
		return nil, fmt.Errorf("%w: run ID is empty", ErrInvalidConfig)
	}
	if err := ValidateDataset(request.Dataset); err != nil {
		return nil, err
	}
	config := NormalizeConfig(request.Config)
	if err := ValidateConfig(config); err != nil {
		return nil, err
	}
	if !skill.IsValidName(request.Skill.Name) || strings.TrimSpace(request.Skill.Body) == "" {
		return nil, fmt.Errorf("%w: skill must have a valid name and non-empty body", ErrInvalidConfig)
	}
	digest, err := DatasetDigest(request.Dataset)
	if err != nil {
		return nil, err
	}
	artifact, err := newArtifact(request.Skill)
	if err != nil {
		return nil, err
	}
	now := e.clock()
	baseline := Revision{ID: revisionID(0, artifact.Digest), Round: 0, Artifact: artifact, CreatedAt: now}
	run := &Run{
		SchemaVersion:       SchemaVersion,
		ID:                  request.RunID,
		Status:              StatusPending,
		Dataset:             cloneDataset(request.Dataset),
		DatasetDigest:       digest,
		Config:              config,
		BaselineContentHash: sourceContentHash(request.Skill),
		BaselineRevisionID:  baseline.ID,
		CurrentRevisionID:   baseline.ID,
		BestRevisionID:      baseline.ID,
		Revisions:           []Revision{baseline},
		NextRound:           1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := e.Store.Create(ctx, run); err != nil {
		return nil, err
	}
	return e.execute(ctx, run)
}

// Run is a convenience spelling for Start.
func (e *Engine) Run(ctx context.Context, request Request) (*Run, error) {
	return e.Start(ctx, request)
}

// Resume continues a pending, paused, or running run from its last durable
// checkpoint. Completed and canceled runs are returned unchanged.
func (e *Engine) Resume(ctx context.Context, runID string) (*Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e == nil || e.Store == nil || e.rollout() == nil || e.Proposer == nil {
		return nil, fmt.Errorf("%w: engine requires store, executor, and proposer", ErrInvalidConfig)
	}
	run, err := e.Store.Load(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == StatusCompleted || run.Status == StatusCanceled || run.Status == StatusBudgetExhausted {
		return run, nil
	}
	if err := ValidateDataset(run.Dataset); err != nil {
		return run, err
	}
	digest, err := DatasetDigest(run.Dataset)
	if err != nil {
		return run, err
	}
	if digest != run.DatasetDigest {
		return run, fmt.Errorf("%w: checkpoint dataset digest mismatch", ErrInvalidDataset)
	}
	if err := ValidateConfig(run.Config); err != nil {
		return run, err
	}
	return e.execute(ctx, run)
}

func (e *Engine) execute(ctx context.Context, run *Run) (*Run, error) {
	if run == nil {
		return nil, fmt.Errorf("%w: nil run", ErrInvalidConfig)
	}
	run.Status = StatusRunning
	run.LastError = ""
	if err := e.Store.Save(ctx, run); err != nil {
		return run, err
	}
	for run.NextRound <= run.Config.MaxRounds {
		if err := e.stopRequested(ctx, run); err != nil {
			return e.finishError(ctx, run, err)
		}
		round := e.ensureRound(run, run.NextRound)
		if err := e.runRound(ctx, run, round); err != nil {
			return e.finishError(ctx, run, err)
		}
		run.NextRound++
		if err := e.Store.Save(ctx, run); err != nil {
			return run, err
		}
	}
	if !run.Test.Completed {
		if err := e.runTest(ctx, run); err != nil {
			return e.finishError(ctx, run, err)
		}
	}
	run.Status = StatusCompleted
	if err := e.Store.Save(ctx, run); err != nil {
		return run, err
	}
	return run, nil
}

func (e *Engine) rollout() RolloutExecutor {
	if e == nil {
		return nil
	}
	if e.Rollout != nil {
		return e.Rollout
	}
	return e.Executor
}

func (e *Engine) ensureRound(run *Run, number int) *RoundRecord {
	for i := range run.Rounds {
		if run.Rounds[i].Number == number {
			return &run.Rounds[i]
		}
	}
	train := sampleTrain(run.Dataset.Train, run.Config.TrainBatchSize, run.Config.Seed, number)
	run.Rounds = append(run.Rounds, RoundRecord{
		Number: number, Stage: StageTraining, IncumbentRevisionID: run.CurrentRevisionID,
		TrainSampleIDs: caseIDs(train), ValidationCaseIDs: caseIDs(run.Dataset.Validation),
	})
	return &run.Rounds[len(run.Rounds)-1]
}

func (e *Engine) runRound(ctx context.Context, run *Run, round *RoundRecord) error {
	if round == nil {
		return fmt.Errorf("%w: nil round", ErrInvalidConfig)
	}
	if round.Completed {
		return nil
	}
	number := round.Number
	base, ok := run.Revision(round.IncumbentRevisionID)
	if !ok {
		return fmt.Errorf("%w: incumbent revision %q missing", ErrInvalidConfig, round.IncumbentRevisionID)
	}
	train, err := casesByID(run.Dataset.Train, round.TrainSampleIDs)
	if err != nil {
		return err
	}
	round.Stage = StageTraining
	if err := e.Store.Save(ctx, run); err != nil {
		return err
	}
	trainRecords, err := e.evaluate(ctx, run, number, PhaseTrain, RoleCurrent, base, train)
	if err != nil {
		return err
	}
	if err := e.stopRequested(ctx, run); err != nil {
		return err
	}

	var candidate Revision
	if round.Proposal == nil {
		round.Stage = StageProposing
		if err := e.Store.Save(ctx, run); err != nil {
			return err
		}
		if !e.budgetAllows(run, Cost{}, true) {
			return ErrBudgetExceeded
		}
		proposal, proposalErr := e.Proposer.Propose(ctx, ProposalRequest{RunID: run.ID, Round: number, Seed: roundSeed(run.Config.Seed, number), Base: base, TrainCases: cloneCases(train), TrainResult: cloneRecords(trainRecords), Limits: run.Config.EditLimits, ModelRef: run.Config.ProposerModelRef})
		run.Usage.Calls++
		if costErr := validateCost(proposal.Cost); costErr == nil {
			addEngineUsage(&run.Usage, proposal.Cost)
		} else if proposalErr == nil {
			proposalErr = costErr
		}
		if proposalErr != nil {
			return proposalErr
		}
		candidateSkill, err := validateAndApplyProposal(base.Artifact.Skill, proposal, run.Config.EditLimits)
		if err != nil {
			return err
		}
		artifact, err := newArtifact(candidateSkill)
		if err != nil {
			return err
		}
		candidate = Revision{ID: revisionID(number, artifact.Digest), ParentID: base.ID, Round: number, Artifact: artifact, CreatedAt: e.clock()}
		// A proposer can return the incumbent body with a different revision ID;
		// retaining the artifact is harmless, but duplicate IDs are not.
		if _, exists := run.Revision(candidate.ID); exists {
			candidate.ID = fmt.Sprintf("%s-%d", candidate.ID, len(run.Revisions))
		}
		run.Revisions = append(run.Revisions, candidate)
		round.CandidateRevisionID = candidate.ID
		round.Proposal = &ProposalRecord{Seed: roundSeed(run.Config.Seed, number), BaseRevisionID: base.ID, CandidateRevisionID: candidate.ID, Edits: append([]BodyEdit(nil), proposal.Edits...), Rationale: redactOptimizationText(proposal.Rationale), ModelRef: proposal.ModelRef, Cost: proposal.Cost, CreatedAt: e.clock()}
		if err := e.Store.Save(ctx, run); err != nil {
			return err
		}
		if e.budgetExceeded(run) {
			return ErrBudgetExceeded
		}
	} else {
		var exists bool
		candidate, exists = run.Revision(round.CandidateRevisionID)
		if !exists {
			return fmt.Errorf("%w: candidate revision %q missing", ErrInvalidConfig, round.CandidateRevisionID)
		}
	}

	round.Stage = StageValidating
	if err := e.Store.Save(ctx, run); err != nil {
		return err
	}
	validation := cloneCases(run.Dataset.Validation)
	baseline, ok := run.Revision(run.BaselineRevisionID)
	if !ok {
		return fmt.Errorf("%w: baseline revision %q missing", ErrInvalidConfig, run.BaselineRevisionID)
	}
	baseResults, err := e.evaluate(ctx, run, number, PhaseValidation, RoleBaseline, baseline, validation)
	if err != nil {
		return err
	}
	currentResults, err := e.evaluate(ctx, run, number, PhaseValidation, RoleCurrent, base, validation)
	if err != nil {
		return err
	}
	candidateResults, err := e.evaluate(ctx, run, number, PhaseValidation, RoleCandidate, candidate, validation)
	if err != nil {
		return err
	}
	round.ValidationCaseIDs = caseIDs(validation)
	round.Stage = StageGating
	if err := e.Store.Save(ctx, run); err != nil {
		return err
	}
	pairs, err := pairResults(validation, baseResults, currentResults, candidateResults)
	if err != nil {
		return err
	}
	decision := e.Gate.Decide(GateInput{CaseIDs: caseIDs(validation), Pairs: pairs, MinDelta: run.Config.MinDelta, Deadband: run.Config.Deadband})
	round.Decision = &decision
	round.Stage = StageComplete
	round.Completed = true
	completedAt := e.clock()
	round.CompletedAt = &completedAt
	if decision.Accepted {
		run.CurrentRevisionID = candidate.ID
		// Best is selected by the gate against current; an accepted candidate is
		// necessarily at least as good under the configured deadband.
		run.BestRevisionID = candidate.ID
	} else {
		run.Rejected = append(run.Rejected, RejectedCandidate{Round: number, RevisionID: candidate.ID, Decision: decision})
	}
	return e.Store.Save(ctx, run)
}

func (e *Engine) runTest(ctx context.Context, run *Run) error {
	revision, ok := run.Revision(run.BestRevisionID)
	if !ok {
		return fmt.Errorf("%w: best revision %q missing", ErrInvalidConfig, run.BestRevisionID)
	}
	started := e.clock()
	run.Test = TestRecord{RevisionID: revision.ID, CaseIDs: caseIDs(run.Dataset.Test), StartedAt: &started}
	if len(run.Dataset.Test) > 0 {
		if _, err := e.evaluate(ctx, run, run.NextRound, PhaseTest, RoleBest, revision, cloneCases(run.Dataset.Test)); err != nil {
			return err
		}
	}
	completed := e.clock()
	run.Test.Completed = true
	run.Test.CompletedAt = &completed
	return nil
}

func (e *Engine) evaluate(ctx context.Context, run *Run, round int, phase Phase, role EvaluationRole, revision Revision, cases []Case) ([]EvaluationRecord, error) {
	results := make([]EvaluationRecord, 0, len(cases))
	for index, sample := range cases {
		if err := e.stopRequested(ctx, run); err != nil {
			return results, err
		}
		seed := caseSeed(run.Config.Seed, round, phase, index)
		if existing, ok := findEvaluation(run.Evaluations, round, phase, role, revision.ID, sample.ID, seed); ok {
			results = append(results, existing)
			continue
		}
		if !e.budgetAllows(run, Cost{}, true) {
			return results, ErrBudgetExceeded
		}
		result, err := e.rollout().Evaluate(ctx, RolloutRequest{RunID: run.ID, Round: round, Phase: phase, Role: role, Case: cloneCase(sample), Skill: cloneSkill(revision.Artifact.Skill), RevisionID: revision.ID, Seed: seed, ModelRef: run.Config.RolloutModelRef})
		run.Usage.Calls++
		if costErr := validateCost(result.Cost); costErr == nil {
			addEngineUsage(&run.Usage, result.Cost)
		} else if err == nil {
			err = costErr
		}
		if err != nil {
			return results, err
		}
		if err := validateResult(result); err != nil {
			return results, err
		}
		result = redactResult(result)
		record := EvaluationRecord{ID: fmt.Sprintf("%d-%s-%s-%s", round, phase, role, sample.ID), Round: round, Phase: phase, Role: role, CaseID: sample.ID, RevisionID: revision.ID, Seed: seed, ModelRef: result.ModelRef, Result: result, CreatedAt: e.clock()}
		run.Evaluations = append(run.Evaluations, record)
		results = append(results, record)
		if err := e.Store.Save(ctx, run); err != nil {
			return results, err
		}
		if e.budgetExceeded(run) {
			return results, ErrBudgetExceeded
		}
	}
	return results, nil
}

func (e *Engine) stopRequested(ctx context.Context, run *Run) error {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ErrCanceled
		}
		return err
	}
	loaded, err := e.Store.Load(ctx, run.ID)
	if err == nil && loaded.CancelRequested {
		return ErrCanceled
	}
	return nil
}

func (e *Engine) finishError(ctx context.Context, run *Run, err error) (*Run, error) {
	run.LastError = redactOptimizationText(err.Error())
	switch {
	case errors.Is(err, ErrCanceled):
		run.Status = StatusCanceled
	case errors.Is(err, ErrBudgetExceeded):
		run.Status = StatusBudgetExhausted
	default:
		run.Status = StatusPaused
	}
	// A canceled/deadline context cannot be used for the final durable write.
	checkpointCtx := ctx
	if errors.Is(err, ErrCanceled) || ctx.Err() != nil {
		checkpointCtx = context.Background()
	}
	if saveErr := e.Store.Save(checkpointCtx, run); saveErr != nil {
		return run, fmt.Errorf("%w (checkpoint: %v)", err, saveErr)
	}
	return run, err
}

func (e *Engine) budgetAllows(run *Run, cost Cost, call bool) bool {
	b := run.Config.Budget
	if call && b.MaxCalls > 0 && run.Usage.Calls+1 > b.MaxCalls {
		return false
	}
	return (b.MaxInputTokens <= 0 || run.Usage.InputTokens+cost.InputTokens <= b.MaxInputTokens) &&
		(b.MaxOutputTokens <= 0 || run.Usage.OutputTokens+cost.OutputTokens <= b.MaxOutputTokens) &&
		(b.MaxAmount <= 0 || run.Usage.Amount+cost.Amount <= b.MaxAmount)
}

func (e *Engine) budgetExceeded(run *Run) bool {
	b := run.Config.Budget
	return (b.MaxCalls > 0 && run.Usage.Calls > b.MaxCalls) ||
		(b.MaxInputTokens > 0 && run.Usage.InputTokens > b.MaxInputTokens) ||
		(b.MaxOutputTokens > 0 && run.Usage.OutputTokens > b.MaxOutputTokens) ||
		(b.MaxAmount > 0 && run.Usage.Amount > b.MaxAmount)
}

func addEngineUsage(usage *Usage, cost Cost) {
	usage.InputTokens += cost.InputTokens
	usage.OutputTokens += cost.OutputTokens
	usage.Amount += cost.Amount
}

func (e *Engine) clock() time.Time {
	if e != nil && e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func revisionID(round int, digest string) string {
	if len(digest) > 16 {
		digest = digest[:16]
	}
	return fmt.Sprintf("r%d-%s", round, digest)
}

func roundSeed(seed int64, round int) int64 {
	// The golden-ratio multiplier is represented as its signed int64 form to
	// avoid an overflowing untyped constant on 32/64-bit Go builds.
	return seed + int64(round)*int64(-7046029254386353131)
}

func caseSeed(seed int64, round int, phase Phase, index int) int64 {
	value := roundSeed(seed, round) + int64(index)*7919
	for _, r := range string(phase) {
		value = value*33 + int64(r)
	}
	return value
}

func sampleTrain(cases []Case, batch int, seed int64, round int) []Case {
	if len(cases) == 0 {
		return nil
	}
	order := make([]int, len(cases))
	for i := range order {
		order[i] = i
	}
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	if batch <= 0 || batch > len(order) {
		batch = len(order)
	}
	start := ((round - 1) * batch) % len(order)
	out := make([]Case, 0, batch)
	for i := 0; i < batch; i++ {
		out = append(out, cloneCase(cases[order[(start+i)%len(order)]]))
	}
	return out
}

func pairResults(cases []Case, baseline, current, candidate []EvaluationRecord) ([]PairedResult, error) {
	base := recordsByCase(baseline)
	cur := recordsByCase(current)
	cand := recordsByCase(candidate)
	out := make([]PairedResult, 0, len(cases))
	for _, sample := range cases {
		b, bok := base[sample.ID]
		c, cok := cur[sample.ID]
		d, dok := cand[sample.ID]
		if !bok || !cok || !dok {
			return nil, fmt.Errorf("validation case %q is missing a paired result", sample.ID)
		}
		out = append(out, PairedResult{CaseID: sample.ID, Baseline: b.Result, Current: c.Result, Candidate: d.Result})
	}
	return out, nil
}

func recordsByCase(records []EvaluationRecord) map[string]EvaluationRecord {
	out := make(map[string]EvaluationRecord, len(records))
	for _, record := range records {
		out[record.CaseID] = record
	}
	return out
}

func findEvaluation(records []EvaluationRecord, round int, phase Phase, role EvaluationRole, revisionID, caseID string, seed int64) (EvaluationRecord, bool) {
	for _, record := range records {
		if record.Round == round && record.Phase == phase && record.Role == role && record.RevisionID == revisionID && record.CaseID == caseID && record.Seed == seed {
			return record, true
		}
	}
	return EvaluationRecord{}, false
}

func caseIDs(cases []Case) []string {
	out := make([]string, 0, len(cases))
	for _, sample := range cases {
		out = append(out, sample.ID)
	}
	return out
}

func casesByID(cases []Case, ids []string) ([]Case, error) {
	byID := make(map[string]Case, len(cases))
	for _, sample := range cases {
		byID[sample.ID] = sample
	}
	out := make([]Case, 0, len(ids))
	for _, id := range ids {
		sample, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: case %q is missing from dataset", ErrInvalidDataset, id)
		}
		out = append(out, cloneCase(sample))
	}
	return out, nil
}

func cloneCase(value Case) Case {
	value.Expected = append(json.RawMessage(nil), value.Expected...)
	if value.Metadata != nil {
		value.Metadata = map[string]string{}
		for key, item := range value.Metadata {
			value.Metadata[key] = item
		}
	}
	return value
}

func cloneRecords(values []EvaluationRecord) []EvaluationRecord {
	out := make([]EvaluationRecord, len(values))
	copy(out, values)
	return out
}

func sourceContentHash(value skill.Skill) string {
	path := strings.TrimSpace(value.Path)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return skill.ContentHash(string(raw))
}
