package skilleval

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"maddog/internal/config"
	"maddog/internal/safety"
)

type CandidateStatus string

const (
	CandidatePending  CandidateStatus = "pending"
	CandidatePromoted CandidateStatus = "promoted"
	CandidateRejected CandidateStatus = "rejected"
)

type SkillSnapshot struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Body         string   `json:"body,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	RunAs        string   `json:"runAs,omitempty"`
	Model        string   `json:"model,omitempty"`
	Effort       string   `json:"effort,omitempty"`
}

type ValidationSnapshot struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

type CandidateInput struct {
	BundleID   string
	Skill      SkillSnapshot
	Validation ValidationSnapshot
	CreatedAt  time.Time
}

type Candidate struct {
	ID           string             `json:"id"`
	Hash         string             `json:"hash"`
	BundleID     string             `json:"bundleId,omitempty"`
	BundleIDs    []string           `json:"bundleIds,omitempty"`
	Status       CandidateStatus    `json:"status"`
	Skill        SkillSnapshot      `json:"skill"`
	Validation   ValidationSnapshot `json:"validation"`
	Evaluation   *EvaluationSummary `json:"evaluation,omitempty"`
	Reason       string             `json:"reason,omitempty"`
	PromotedPath string             `json:"promotedPath,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

type EvaluationSummary struct {
	CandidateID         string    `json:"candidateId"`
	Decision            string    `json:"decision"`
	Score               float64   `json:"score,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	ReplayCases         int       `json:"replayCases,omitempty"`
	HeldOutCases        int       `json:"heldOutCases,omitempty"`
	BaselinePassRate    float64   `json:"baselinePassRate,omitempty"`
	CandidatePassRate   float64   `json:"candidatePassRate,omitempty"`
	TokenDeltaPercent   float64   `json:"tokenDeltaPercent,omitempty"`
	FrontierUnavailable bool      `json:"frontierUnavailable,omitempty"`
	EvaluatedAt         time.Time `json:"evaluatedAt"`
}

type Store struct {
	root     string
	redactor safety.Redactor
}

type GeneratedSkillRecord struct {
	Task       string
	Source     string
	Snapshot   map[string]any
	RawRefs    []RawRefMetadata
	Skill      SkillSnapshot
	Validation ValidationSnapshot
	CreatedAt  time.Time
}

type GeneratedSkillResult struct {
	Bundle    Bundle
	Candidate Candidate
	Created   bool
}

func NewStore(root string) *Store {
	return &Store{root: root, redactor: safety.DefaultRedactor()}
}

func NewProjectStore(projectRoot string) *Store {
	return NewStore(ProjectStoreRoot(projectRoot))
}

func ProjectStoreRoot(projectRoot string) string {
	if strings.TrimSpace(projectRoot) == "" {
		return ""
	}
	return filepath.Join(projectRoot, config.ProjectConventionDir, "skilleval")
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *Store) BundlePath(id string) string {
	return filepath.Join(s.root, "bundles", safeFileID(id)+".json")
}

func (s *Store) CandidatePath(id string) string {
	return filepath.Join(s.root, "candidates", safeFileID(id)+".json")
}

func (s *Store) RecordGeneratedSkill(record GeneratedSkillRecord) (GeneratedSkillResult, error) {
	if s == nil {
		return GeneratedSkillResult{}, errors.New("skilleval store is nil")
	}
	bundle := BuildBundle(BundleInput{
		Task:      record.Task,
		Source:    record.Source,
		Snapshot:  record.Snapshot,
		RawRefs:   record.RawRefs,
		CreatedAt: record.CreatedAt,
	})
	if err := s.WriteBundle(bundle); err != nil {
		return GeneratedSkillResult{}, err
	}
	candidate, created, err := s.AddCandidate(CandidateInput{
		BundleID:   bundle.ID,
		Skill:      record.Skill,
		Validation: record.Validation,
		CreatedAt:  record.CreatedAt,
	})
	if err != nil {
		return GeneratedSkillResult{}, err
	}
	return GeneratedSkillResult{Bundle: bundle, Candidate: candidate, Created: created}, nil
}

func (s *Store) WriteBundle(bundle Bundle) error {
	if s == nil {
		return errors.New("skilleval store is nil")
	}
	if strings.TrimSpace(s.root) == "" {
		return errors.New("skilleval store root is empty")
	}
	if strings.TrimSpace(bundle.ID) == "" {
		return errors.New("bundle id is empty")
	}
	return writeJSON(s.BundlePath(bundle.ID), bundle)
}

func (s *Store) ReadBundle(id string) (Bundle, bool, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return Bundle{}, false, nil
	}
	body, err := os.ReadFile(s.BundlePath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return Bundle{}, false, nil
		}
		return Bundle{}, false, err
	}
	var bundle Bundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return Bundle{}, false, err
	}
	return bundle, true, nil
}

func (s *Store) AddCandidate(input CandidateInput) (Candidate, bool, error) {
	if s == nil {
		return Candidate{}, false, errors.New("skilleval store is nil")
	}
	if strings.TrimSpace(s.root) == "" {
		return Candidate{}, false, errors.New("skilleval store root is empty")
	}
	candidate := BuildCandidate(input)
	path := s.CandidatePath(candidate.ID)
	existing, ok, err := s.ReadCandidate(candidate.ID)
	if err != nil {
		return Candidate{}, false, err
	}
	if ok {
		existing.BundleIDs = appendUnique(existing.BundleIDs, candidate.BundleIDs...)
		if existing.BundleID == "" && len(existing.BundleIDs) > 0 {
			existing.BundleID = existing.BundleIDs[0]
		}
		existing.UpdatedAt = time.Now().UTC()
		if err := writeJSON(path, existing); err != nil {
			return Candidate{}, false, err
		}
		return existing, false, nil
	}
	if err := writeJSON(path, candidate); err != nil {
		return Candidate{}, false, err
	}
	return candidate, true, nil
}

func BuildCandidate(input CandidateInput) Candidate {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	hash := CandidateHash(input.Skill)
	status := CandidatePending
	if !input.Validation.Valid {
		status = CandidateRejected
	}
	bundleIDs := appendUnique(nil, strings.TrimSpace(input.BundleID))
	redactor := safety.DefaultRedactor()
	return Candidate{
		ID:        "cand-" + hash[:16],
		Hash:      hash,
		BundleID:  first(bundleIDs),
		BundleIDs: bundleIDs,
		Status:    status,
		Skill: SkillSnapshot{
			Name:         redactor.String(strings.TrimSpace(input.Skill.Name)),
			Description:  redactor.String(strings.TrimSpace(input.Skill.Description)),
			Body:         redactor.String(strings.TrimSpace(input.Skill.Body)),
			AllowedTools: redactStrings(redactor, input.Skill.AllowedTools),
			RunAs:        redactor.String(strings.TrimSpace(input.Skill.RunAs)),
			Model:        redactor.String(strings.TrimSpace(input.Skill.Model)),
			Effort:       redactor.String(strings.TrimSpace(input.Skill.Effort)),
		},
		Validation: ValidationSnapshot{
			Valid:  input.Validation.Valid,
			Reason: redactor.String(strings.TrimSpace(input.Validation.Reason)),
		},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

func CandidateHash(skill SkillSnapshot) string {
	tools := append([]string(nil), skill.AllowedTools...)
	for i := range tools {
		tools[i] = strings.TrimSpace(tools[i])
	}
	sort.Strings(tools)
	payload := SkillSnapshot{
		Name:         strings.TrimSpace(skill.Name),
		Description:  strings.TrimSpace(skill.Description),
		Body:         strings.TrimSpace(skill.Body),
		AllowedTools: tools,
		RunAs:        strings.TrimSpace(skill.RunAs),
		Model:        strings.TrimSpace(skill.Model),
		Effort:       strings.TrimSpace(skill.Effort),
	}
	body, _ := json.Marshal(payload)
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}

func (s *Store) ReadCandidate(id string) (Candidate, bool, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return Candidate{}, false, nil
	}
	body, err := os.ReadFile(s.CandidatePath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return Candidate{}, false, nil
		}
		return Candidate{}, false, err
	}
	var candidate Candidate
	if err := json.Unmarshal(body, &candidate); err != nil {
		return Candidate{}, false, err
	}
	return candidate, true, nil
}

func (s *Store) UpdateCandidateEvaluation(id string, summary EvaluationSummary) (Candidate, error) {
	candidate, ok, err := s.ReadCandidate(id)
	if err != nil {
		return Candidate{}, err
	}
	if !ok {
		return Candidate{}, fmt.Errorf("candidate %q not found", id)
	}
	if summary.CandidateID == "" {
		summary.CandidateID = candidate.ID
	}
	if summary.EvaluatedAt.IsZero() {
		summary.EvaluatedAt = time.Now().UTC()
	}
	candidate.Evaluation = &summary
	candidate.UpdatedAt = time.Now().UTC()
	if err := writeJSON(s.CandidatePath(candidate.ID), candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

func (s *Store) UpdateCandidateStatus(id string, status CandidateStatus, reason, promotedPath string) (Candidate, error) {
	candidate, ok, err := s.ReadCandidate(id)
	if err != nil {
		return Candidate{}, err
	}
	if !ok {
		return Candidate{}, fmt.Errorf("candidate %q not found", id)
	}
	candidate.Status = status
	candidate.Reason = safety.RedactString(strings.TrimSpace(reason))
	candidate.PromotedPath = safety.RedactString(strings.TrimSpace(promotedPath))
	candidate.UpdatedAt = time.Now().UTC()
	if err := writeJSON(s.CandidatePath(candidate.ID), candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

func (s *Store) ListCandidates() ([]Candidate, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return nil, nil
	}
	dir := filepath.Join(s.root, "candidates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var candidate Candidate
		if err := json.Unmarshal(body, &candidate); err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

func appendUnique(base []string, values ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(values))
	for _, v := range append(base, values...) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func redactStrings(redactor safety.Redactor, in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if v := strings.TrimSpace(redactor.String(s)); v != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func safeFileID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "_"
	}
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
