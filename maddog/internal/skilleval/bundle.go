package skilleval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"maddog/internal/safety"
)

const BundleSchemaVersion = 2

const (
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
)

type BundleInput struct {
	Task                string
	Source              string
	Snapshot            map[string]any
	RawRefs             []RawRefMetadata
	CandidateDocs       []CandidateDoc
	CuratedEvidence     []CuratedEvidence
	VerificationRecords []VerificationRecord
	Trajectory          []ActionObservation
	BudgetContext       ReplayBudgetContext
	CreatedAt           time.Time
}

type RawRefMetadata struct {
	Ref             string `json:"ref"`
	Source          string `json:"source,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
	Available       bool   `json:"available,omitempty"`
	OriginalBytes   int    `json:"originalBytes,omitempty"`
	CompressedBytes int    `json:"compressedBytes,omitempty"`
}

type CandidateDoc struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type CuratedEvidence struct {
	ID      string `json:"id"`
	Kind    string `json:"kind,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type VerificationRecord struct {
	ID      string `json:"id"`
	Command string `json:"command,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type ActionObservation struct {
	StepID      string `json:"stepId"`
	Action      string `json:"action,omitempty"`
	Observation string `json:"observation,omitempty"`
	Status      string `json:"status,omitempty"`
}

type ReplayBudgetContext struct {
	LimitTokens     int64   `json:"limitTokens,omitempty"`
	UsedTokens      int64   `json:"usedTokens,omitempty"`
	RemainingTokens int64   `json:"remainingTokens,omitempty"`
	Cost            float64 `json:"cost,omitempty"`
	Currency        string  `json:"currency,omitempty"`
}

type Bundle struct {
	ID                  string               `json:"id"`
	SchemaVersion       int                  `json:"schemaVersion"`
	Task                string               `json:"task,omitempty"`
	Source              string               `json:"source,omitempty"`
	Confidence          string               `json:"confidence"`
	LowConfidence       bool                 `json:"lowConfidence,omitempty"`
	Snapshot            map[string]any       `json:"snapshot,omitempty"`
	RawRefs             []RawRefMetadata     `json:"rawRefs,omitempty"`
	CandidateDocs       []CandidateDoc       `json:"candidateDocs,omitempty"`
	CuratedEvidence     []CuratedEvidence    `json:"curatedEvidence,omitempty"`
	VerificationRecords []VerificationRecord `json:"verificationRecords,omitempty"`
	Trajectory          []ActionObservation  `json:"trajectory,omitempty"`
	BudgetContext       ReplayBudgetContext  `json:"budgetContext,omitempty"`
	CreatedAt           time.Time            `json:"createdAt"`
}

func BuildBundle(input BundleInput) Bundle {
	redactor := safety.DefaultRedactor()
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	snapshot := redactor.Map(input.Snapshot)
	rawRefs := make([]RawRefMetadata, 0, len(input.RawRefs))
	for _, ref := range input.RawRefs {
		rawRefs = append(rawRefs, RawRefMetadata{
			Ref:             redactor.String(strings.TrimSpace(ref.Ref)),
			Source:          redactor.String(strings.TrimSpace(ref.Source)),
			ToolName:        redactor.String(strings.TrimSpace(ref.ToolName)),
			Available:       ref.Available,
			OriginalBytes:   ref.OriginalBytes,
			CompressedBytes: ref.CompressedBytes,
		})
	}
	candidateDocs := redactCandidateDocs(redactor, input.CandidateDocs)
	curatedEvidence := redactCuratedEvidence(redactor, input.CuratedEvidence)
	verificationRecords := redactVerificationRecords(redactor, input.VerificationRecords)
	trajectory := redactTrajectory(redactor, input.Trajectory)
	budgetContext := input.BudgetContext
	budgetContext.Currency = redactor.String(strings.TrimSpace(budgetContext.Currency))
	low := len(rawRefs) == 0 && len(snapshot) == 0 && len(candidateDocs) == 0 && len(curatedEvidence) == 0 && len(verificationRecords) == 0 && len(trajectory) == 0
	confidence := ConfidenceMedium
	if low {
		confidence = ConfidenceLow
	}
	bundle := Bundle{
		SchemaVersion:       BundleSchemaVersion,
		Task:                redactor.String(strings.TrimSpace(input.Task)),
		Source:              redactor.String(strings.TrimSpace(input.Source)),
		Confidence:          confidence,
		LowConfidence:       low,
		Snapshot:            snapshot,
		RawRefs:             rawRefs,
		CandidateDocs:       candidateDocs,
		CuratedEvidence:     curatedEvidence,
		VerificationRecords: verificationRecords,
		Trajectory:          trajectory,
		BudgetContext:       budgetContext,
		CreatedAt:           createdAt,
	}
	bundle.ID = "bundle-" + shortHash(bundleHashPayload{
		SchemaVersion:       bundle.SchemaVersion,
		Task:                bundle.Task,
		Source:              bundle.Source,
		Confidence:          bundle.Confidence,
		Snapshot:            bundle.Snapshot,
		RawRefs:             bundle.RawRefs,
		CandidateDocs:       bundle.CandidateDocs,
		CuratedEvidence:     bundle.CuratedEvidence,
		VerificationRecords: bundle.VerificationRecords,
		Trajectory:          bundle.Trajectory,
		BudgetContext:       bundle.BudgetContext,
	})
	return bundle
}

type bundleHashPayload struct {
	SchemaVersion       int                  `json:"schemaVersion"`
	Task                string               `json:"task,omitempty"`
	Source              string               `json:"source,omitempty"`
	Confidence          string               `json:"confidence"`
	Snapshot            map[string]any       `json:"snapshot,omitempty"`
	RawRefs             []RawRefMetadata     `json:"rawRefs,omitempty"`
	CandidateDocs       []CandidateDoc       `json:"candidateDocs,omitempty"`
	CuratedEvidence     []CuratedEvidence    `json:"curatedEvidence,omitempty"`
	VerificationRecords []VerificationRecord `json:"verificationRecords,omitempty"`
	Trajectory          []ActionObservation  `json:"trajectory,omitempty"`
	BudgetContext       ReplayBudgetContext  `json:"budgetContext,omitempty"`
}

func redactCandidateDocs(redactor safety.Redactor, docs []CandidateDoc) []CandidateDoc {
	out := make([]CandidateDoc, 0, len(docs))
	for _, doc := range docs {
		out = append(out, CandidateDoc{
			ID:      redactor.String(strings.TrimSpace(doc.ID)),
			Title:   redactor.String(strings.TrimSpace(doc.Title)),
			Ref:     redactor.String(strings.TrimSpace(doc.Ref)),
			Summary: redactor.String(strings.TrimSpace(doc.Summary)),
		})
	}
	return out
}

func redactCuratedEvidence(redactor safety.Redactor, items []CuratedEvidence) []CuratedEvidence {
	out := make([]CuratedEvidence, 0, len(items))
	for _, item := range items {
		out = append(out, CuratedEvidence{
			ID:      redactor.String(strings.TrimSpace(item.ID)),
			Kind:    redactor.String(strings.TrimSpace(item.Kind)),
			Ref:     redactor.String(strings.TrimSpace(item.Ref)),
			Summary: redactor.String(strings.TrimSpace(item.Summary)),
		})
	}
	return out
}

func redactVerificationRecords(redactor safety.Redactor, records []VerificationRecord) []VerificationRecord {
	out := make([]VerificationRecord, 0, len(records))
	for _, record := range records {
		out = append(out, VerificationRecord{
			ID:      redactor.String(strings.TrimSpace(record.ID)),
			Command: redactor.String(strings.TrimSpace(record.Command)),
			Status:  redactor.String(strings.TrimSpace(record.Status)),
			Summary: redactor.String(strings.TrimSpace(record.Summary)),
		})
	}
	return out
}

func redactTrajectory(redactor safety.Redactor, steps []ActionObservation) []ActionObservation {
	out := make([]ActionObservation, 0, len(steps))
	for _, step := range steps {
		out = append(out, ActionObservation{
			StepID:      redactor.String(strings.TrimSpace(step.StepID)),
			Action:      redactor.String(strings.TrimSpace(step.Action)),
			Observation: redactor.String(strings.TrimSpace(step.Observation)),
			Status:      redactor.String(strings.TrimSpace(step.Status)),
		})
	}
	return out
}

func shortHash(v any) string {
	body, _ := json.Marshal(v)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:16]
}
