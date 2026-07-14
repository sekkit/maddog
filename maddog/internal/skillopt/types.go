// Package skillopt implements a bounded, resumable optimization loop for
// maddog skills. Rollouts and proposals are deliberately expressed as small
// interfaces so production providers and deterministic local fixtures use the
// same engine.
package skillopt

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"maddog/internal/skill"
)

const SchemaVersion = 1

var (
	ErrRunNotFound        = errors.New("skillopt run not found")
	ErrRunExists          = errors.New("skillopt run already exists")
	ErrCanceled           = errors.New("skillopt run canceled")
	ErrBudgetExceeded     = errors.New("skillopt budget exceeded")
	ErrInvalidDataset     = errors.New("invalid skillopt dataset")
	ErrInvalidConfig      = errors.New("invalid skillopt config")
	ErrInvalidProposal    = errors.New("invalid skillopt proposal")
	ErrCapabilityMutation = errors.New("skill capability mutation rejected")
)

// Case is one immutable optimization example. Expected and Metadata are
// opaque to the engine and are interpreted by the RolloutExecutor.
type Case struct {
	ID       string            `json:"id" toml:"id"`
	Input    string            `json:"input" toml:"input"`
	Expected json.RawMessage   `json:"expected,omitempty" toml:"expected"`
	Metadata map[string]string `json:"metadata,omitempty" toml:"metadata"`
}

// Dataset keeps the three splits explicit. A run stores the complete snapshot
// and rejects duplicate case IDs across splits.
type Dataset struct {
	ID         string `json:"id" toml:"id"`
	Train      []Case `json:"train" toml:"train"`
	Validation []Case `json:"validation" toml:"validation"`
	Test       []Case `json:"test" toml:"test"`
}

type Phase string

const (
	PhaseTrain      Phase = "train"
	PhaseValidation Phase = "validation"
	PhaseTest       Phase = "test"
)

type EvaluationRole string

const (
	RoleBaseline  EvaluationRole = "baseline"
	RoleCurrent   EvaluationRole = "current"
	RoleCandidate EvaluationRole = "candidate"
	RoleBest      EvaluationRole = "best"
)

// Cost is reported by one model-backed operation.
type Cost struct {
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	Amount       float64 `json:"amount,omitempty"`
}

// Usage is the cumulative budget ledger. Calls includes both rollout and
// proposer invocations, including failed invocations that reached an adapter.
type Usage struct {
	Calls        int     `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Amount       float64 `json:"amount"`
}

// Result contains the three objective dimensions retained for every case.
// Hard is the verifier outcome; Soft is a normalized [0,1] quality score; Cost
// is accounted independently and never hidden inside the quality score.
type Result struct {
	Hard     bool            `json:"hard"`
	Soft     float64         `json:"soft"`
	Cost     Cost            `json:"cost"`
	Output   string          `json:"output,omitempty"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
	ModelRef string          `json:"model_ref,omitempty"`
}

// RolloutRequest is one isolated case execution.
type RolloutRequest struct {
	RunID      string
	Round      int
	Phase      Phase
	Role       EvaluationRole
	Case       Case
	Skill      skill.Skill
	RevisionID string
	Seed       int64
	ModelRef   string
}

type RolloutExecutor interface {
	Evaluate(context.Context, RolloutRequest) (Result, error)
}

// BodyEdit uses byte offsets into the incumbent body. Edits must be ordered,
// non-overlapping, and within the configured edit and byte budgets.
type BodyEdit struct {
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Replacement string `json:"replacement"`
}

type EditLimits struct {
	MaxEdits        int `json:"max_edits"`
	MaxChangedBytes int `json:"max_changed_bytes"`
	MaxBodyBytes    int `json:"max_body_bytes"`
}

type ProposalRequest struct {
	RunID       string
	Round       int
	Seed        int64
	Base        Revision
	TrainCases  []Case
	TrainResult []EvaluationRecord
	Limits      EditLimits
	ModelRef    string
}

// Proposal carries both a structured patch and the complete resulting skill.
// The engine replays Edits and requires Candidate.Body to match. Every other
// skill field must be byte-for-byte equivalent to the incumbent.
type Proposal struct {
	Candidate skill.Skill `json:"candidate"`
	Edits     []BodyEdit  `json:"edits"`
	Rationale string      `json:"rationale,omitempty"`
	ModelRef  string      `json:"model_ref,omitempty"`
	Cost      Cost        `json:"cost"`
}

type Proposer interface {
	Propose(context.Context, ProposalRequest) (Proposal, error)
}

// Optimizer is the semantic name used by callers that treat proposal
// generation as an optimization policy.
type Optimizer interface {
	Proposer
}

type Artifact struct {
	Skill  skill.Skill `json:"skill"`
	Digest string      `json:"digest"`
}

// Revision is append-only. Current and Best are IDs into Run.Revisions rather
// than mutable skill copies.
type Revision struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Round     int       `json:"round"`
	Artifact  Artifact  `json:"artifact"`
	CreatedAt time.Time `json:"created_at"`
}

type EvaluationRecord struct {
	ID         string         `json:"id"`
	Round      int            `json:"round"`
	Phase      Phase          `json:"phase"`
	Role       EvaluationRole `json:"role"`
	CaseID     string         `json:"case_id"`
	RevisionID string         `json:"revision_id"`
	Seed       int64          `json:"seed"`
	ModelRef   string         `json:"model_ref,omitempty"`
	Result     Result         `json:"result"`
	CreatedAt  time.Time      `json:"created_at"`
}

type ProposalRecord struct {
	Seed                int64      `json:"seed"`
	BaseRevisionID      string     `json:"base_revision_id"`
	CandidateRevisionID string     `json:"candidate_revision_id"`
	Edits               []BodyEdit `json:"edits"`
	Rationale           string     `json:"rationale,omitempty"`
	ModelRef            string     `json:"model_ref,omitempty"`
	Cost                Cost       `json:"cost"`
	CreatedAt           time.Time  `json:"created_at"`
}

type Aggregate struct {
	Cases     int     `json:"cases"`
	HardRate  float64 `json:"hard_rate"`
	SoftMean  float64 `json:"soft_mean"`
	CostTotal Cost    `json:"cost_total"`
}

type PairedResult struct {
	CaseID    string `json:"case_id"`
	Baseline  Result `json:"baseline"`
	Current   Result `json:"current"`
	Candidate Result `json:"candidate"`
}

type GateInput struct {
	CaseIDs  []string
	Pairs    []PairedResult
	MinDelta float64
	Deadband float64
}

type Decision struct {
	Accepted       bool      `json:"accepted"`
	Reason         string    `json:"reason"`
	CaseIDs        []string  `json:"case_ids"`
	Baseline       Aggregate `json:"baseline"`
	Current        Aggregate `json:"current"`
	Candidate      Aggregate `json:"candidate"`
	HardDelta      float64   `json:"hard_delta"`
	SoftDelta      float64   `json:"soft_delta"`
	DecisiveMetric string    `json:"decisive_metric"`
}

type Gate interface {
	Decide(GateInput) Decision
}

type Budget struct {
	MaxCalls        int     `json:"max_calls,omitempty"`
	MaxInputTokens  int64   `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int64   `json:"max_output_tokens,omitempty"`
	MaxAmount       float64 `json:"max_amount,omitempty"`
}

type Config struct {
	MaxRounds        int        `json:"max_rounds"`
	TrainBatchSize   int        `json:"train_batch_size"`
	Seed             int64      `json:"seed"`
	MinDelta         float64    `json:"min_delta"`
	Deadband         float64    `json:"deadband"`
	EditLimits       EditLimits `json:"edit_limits"`
	Budget           Budget     `json:"budget"`
	MaxConcurrency   int        `json:"max_concurrency"`
	RolloutModelRef  string     `json:"rollout_model_ref,omitempty"`
	ProposerModelRef string     `json:"proposer_model_ref,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		MaxRounds:      3,
		TrainBatchSize: 4,
		Seed:           1,
		MinDelta:       0.01,
		Deadband:       0.001,
		EditLimits: EditLimits{
			MaxEdits:        4,
			MaxChangedBytes: 4096,
			MaxBodyBytes:    64 << 10,
		},
		MaxConcurrency: 1,
	}
}

type Request struct {
	RunID   string
	Dataset Dataset
	Skill   skill.Skill
	Config  Config
}

type RunStatus string

const (
	StatusPending         RunStatus = "pending"
	StatusRunning         RunStatus = "running"
	StatusPaused          RunStatus = "paused"
	StatusCompleted       RunStatus = "completed"
	StatusCanceled        RunStatus = "canceled"
	StatusBudgetExhausted RunStatus = "budget_exhausted"
)

type RoundStage string

const (
	StageTraining   RoundStage = "training"
	StageProposing  RoundStage = "proposing"
	StageValidating RoundStage = "validating"
	StageGating     RoundStage = "gating"
	StageComplete   RoundStage = "complete"
)

type RoundRecord struct {
	Number              int             `json:"number"`
	Stage               RoundStage      `json:"stage"`
	TrainSampleIDs      []string        `json:"train_sample_ids"`
	IncumbentRevisionID string          `json:"incumbent_revision_id"`
	CandidateRevisionID string          `json:"candidate_revision_id,omitempty"`
	ValidationCaseIDs   []string        `json:"validation_case_ids"`
	Proposal            *ProposalRecord `json:"proposal,omitempty"`
	Decision            *Decision       `json:"decision,omitempty"`
	Completed           bool            `json:"completed"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty"`
}

type RejectedCandidate struct {
	Round      int      `json:"round"`
	RevisionID string   `json:"revision_id"`
	Decision   Decision `json:"decision"`
}

type SamplerState struct {
	Epoch    int      `json:"epoch"`
	Position int      `json:"position"`
	Order    []string `json:"order"`
}

type TestRecord struct {
	RevisionID  string     `json:"revision_id,omitempty"`
	CaseIDs     []string   `json:"case_ids,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PromotionRecord is written only by an explicit lifecycle command after a
// completed run. Previous preserves the exact bytes required for CAS rollback.
type PromotionRecord struct {
	RevisionID     string                 `json:"revision_id"`
	Scope          skill.Scope            `json:"scope"`
	Path           string                 `json:"path"`
	BaseHash       string                 `json:"base_hash,omitempty"`
	PromotedHash   string                 `json:"promoted_hash"`
	Previous       *skill.VersionSnapshot `json:"previous,omitempty"`
	PromotedAt     time.Time              `json:"promoted_at"`
	RolledBack     bool                   `json:"rolled_back,omitempty"`
	RolledBackAt   *time.Time             `json:"rolled_back_at,omitempty"`
	RollbackReason string                 `json:"rollback_reason,omitempty"`
}

// Run is the complete durable checkpoint.
type Run struct {
	SchemaVersion       int                 `json:"schema_version"`
	ID                  string              `json:"id"`
	Status              RunStatus           `json:"status"`
	CancelRequested     bool                `json:"cancel_requested"`
	Dataset             Dataset             `json:"dataset"`
	DatasetDigest       string              `json:"dataset_digest"`
	Config              Config              `json:"config"`
	BaselineContentHash string              `json:"baseline_content_hash,omitempty"`
	BaselineRevisionID  string              `json:"baseline_revision_id"`
	CurrentRevisionID   string              `json:"current_revision_id"`
	BestRevisionID      string              `json:"best_revision_id"`
	Revisions           []Revision          `json:"revisions"`
	Rounds              []RoundRecord       `json:"rounds"`
	NextRound           int                 `json:"next_round"`
	Rejected            []RejectedCandidate `json:"rejected"`
	Evaluations         []EvaluationRecord  `json:"evaluations"`
	Sampler             SamplerState        `json:"sampler"`
	Test                TestRecord          `json:"test"`
	Promotion           *PromotionRecord    `json:"promotion,omitempty"`
	Usage               Usage               `json:"usage"`
	LastError           string              `json:"last_error,omitempty"`
	Checkpoint          uint64              `json:"checkpoint"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
}

func (r *Run) Revision(id string) (Revision, bool) {
	for _, revision := range r.Revisions {
		if revision.ID == id {
			return revision, true
		}
	}
	return Revision{}, false
}

type RunStore interface {
	Create(context.Context, *Run) error
	Load(context.Context, string) (*Run, error)
	Save(context.Context, *Run) error
	Status(context.Context, string) (RunStatus, error)
	Cancel(context.Context, string) error
}
