package skilleval

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"maddog/internal/eval"
	"maddog/internal/fileutil"
	"maddog/internal/skill"
)

type CandidateStatus string

const (
	CandidatePending    CandidateStatus = "pending"
	CandidatePromoting  CandidateStatus = "promoting"
	CandidateRejected   CandidateStatus = "rejected"
	CandidatePromoted   CandidateStatus = "promoted"
	CandidateRolledBack CandidateStatus = "rolled_back"
)

type CandidateStore struct {
	Dir string
	Now func() time.Time
}

type Candidate struct {
	Hash                string          `json:"hash"`
	Skill               skill.Skill     `json:"skill"`
	Status              CandidateStatus `json:"status"`
	SourceBundleID      string          `json:"source_bundle_id,omitempty"`
	SourceBundlePath    string          `json:"source_bundle_path,omitempty"`
	SourceTask          string          `json:"source_task,omitempty"`
	Validation          ValidationInfo  `json:"validation"`
	ValidationReason    string          `json:"validation_reason,omitempty"`
	EvalScore           *ScoreResult    `json:"eval_score,omitempty"`
	EvaluatedHash       string          `json:"evaluated_hash,omitempty"`
	GuardrailPass       bool            `json:"guardrail_pass,omitempty"`
	GuardrailReason     string          `json:"guardrail_reason,omitempty"`
	EvaluationMode      string          `json:"evaluation_mode,omitempty"`
	EvaluationProvider  string          `json:"evaluation_provider,omitempty"`
	EvaluationModelRef  string          `json:"evaluation_model_ref,omitempty"`
	EvaluationBundleIDs []string        `json:"evaluation_bundle_ids,omitempty"`
	PromotionGrade      bool            `json:"promotion_grade,omitempty"`
	PromotedPath        string          `json:"promoted_path,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type AuditRecord struct {
	Time   time.Time       `json:"time"`
	Action string          `json:"action"`
	Hash   string          `json:"hash"`
	Name   string          `json:"name"`
	Status CandidateStatus `json:"status"`
	Path   string          `json:"path,omitempty"`
	Reason string          `json:"reason,omitempty"`
}

type ValidationInfo struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

func NewCandidateStore(dir string) *CandidateStore {
	return &CandidateStore{Dir: dir, Now: time.Now}
}

func (s *CandidateStore) Get(hash string) (Candidate, error) {
	if s == nil {
		return Candidate{}, fmt.Errorf("candidate store is nil")
	}
	return s.load(hash)
}

func (s *CandidateStore) List() ([]Candidate, error) {
	if s == nil {
		return nil, fmt.Errorf("candidate store is nil")
	}
	dir := filepath.Join(s.Dir, "candidates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Candidate{}, nil
		}
		return nil, err
	}
	out := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		hash := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !isCandidateHash(hash) {
			continue
		}
		c, err := s.load(hash)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].Hash < out[j].Hash
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *CandidateStore) AuditForHash(hash string) ([]AuditRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("candidate store is nil")
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return []AuditRecord{}, nil
	}
	f, err := os.Open(filepath.Join(s.Dir, "audit.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return []AuditRecord{}, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []AuditRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record AuditRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.Hash == hash {
			out = append(out, record)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *CandidateStore) Create(sk skill.Skill, bundle BundleV2, task string) (Candidate, error) {
	if s == nil {
		return Candidate{}, fmt.Errorf("candidate store is nil")
	}
	hash := candidateHash(sk)
	path := s.path(hash)
	if existing, err := s.load(hash); err == nil {
		verdict := skill.NewValidator().Validate(sk, task)
		if !verdict.Valid {
			existing.Validation = ValidationInfo{Valid: false, Reason: verdict.Reason}
			existing.ValidationReason = verdict.Reason
			if existing.Status == CandidatePending {
				existing.Status = CandidateRejected
				existing.UpdatedAt = s.now()
				if err := writeJSON(path, existing); err != nil {
					return Candidate{}, err
				}
			}
			if existing.Status == CandidatePromoted {
				copy := existing
				copy.Status = CandidateRejected
				return copy, nil
			}
		}
		return existing, nil
	} else if !os.IsNotExist(err) {
		return Candidate{}, err
	}
	now := s.now()
	verdict := skill.NewValidator().Validate(sk, task)
	status := CandidatePending
	reason := ""
	if !verdict.Valid {
		status = CandidateRejected
		reason = verdict.Reason
	}
	c := Candidate{
		Hash:             hash,
		Skill:            sk,
		Status:           status,
		SourceBundleID:   strings.TrimSpace(bundle.ID),
		SourceBundlePath: strings.TrimSpace(bundle.Path),
		SourceTask:       strings.TrimSpace(task),
		Validation:       ValidationInfo{Valid: verdict.Valid, Reason: verdict.Reason},
		ValidationReason: reason,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Candidate{}, err
	}
	if err := writeNewJSON(path, c); err != nil {
		if os.IsExist(err) {
			return s.load(hash)
		}
		return Candidate{}, err
	}
	return c, nil
}

func (s *CandidateStore) Reject(hash, reason string) (Candidate, error) {
	if s == nil {
		return Candidate{}, fmt.Errorf("candidate store is nil")
	}
	c, err := s.load(hash)
	if err != nil {
		return Candidate{}, err
	}
	if err := c.verifyHash(hash); err != nil {
		return Candidate{}, err
	}
	if c.Status == CandidatePromoted {
		return Candidate{}, fmt.Errorf("candidate %s is already promoted", c.Hash)
	}
	c.Status = CandidateRejected
	c.Validation.Valid = false
	c.Validation.Reason = strings.TrimSpace(reason)
	c.ValidationReason = strings.TrimSpace(reason)
	c.UpdatedAt = s.now()
	if err := writeJSON(s.path(c.Hash), c); err != nil {
		return Candidate{}, err
	}
	_ = s.appendAudit("reject", c, "", c.ValidationReason)
	return c, nil
}

type EvaluationProvenance struct {
	Mode           string
	Provider       string
	ModelRef       string
	BundleIDs      []string
	PromotionGrade bool
}

const (
	EvaluationModeDryRunPreview  = "dry_run_preview"
	EvaluationModeProviderReplay = "provider_replay"
)

func (s *CandidateStore) RecordEvaluation(hash string, score ScoreResult, guardrail GuardrailResult) (Candidate, error) {
	return s.RecordEvaluationWithProvenance(hash, score, guardrail, EvaluationProvenance{})
}

func (s *CandidateStore) RecordEvaluationWithProvenance(hash string, score ScoreResult, guardrail GuardrailResult, provenance EvaluationProvenance) (Candidate, error) {
	if s == nil {
		return Candidate{}, fmt.Errorf("candidate store is nil")
	}
	c, err := s.load(hash)
	if err != nil {
		return Candidate{}, err
	}
	if err := c.verifyHash(hash); err != nil {
		return Candidate{}, err
	}
	c.EvalScore = &score
	c.EvaluatedHash = c.Hash
	c.GuardrailPass = guardrail.Pass
	c.GuardrailReason = strings.TrimSpace(guardrail.Reason)
	c.EvaluationMode = strings.TrimSpace(provenance.Mode)
	c.EvaluationProvider = strings.TrimSpace(provenance.Provider)
	c.EvaluationModelRef = strings.TrimSpace(provenance.ModelRef)
	c.EvaluationBundleIDs = cleanStringList(provenance.BundleIDs)
	c.PromotionGrade = provenance.PromotionGrade
	c.UpdatedAt = s.now()
	if err := writeJSON(s.path(c.Hash), c); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func (s *CandidateStore) Promote(hash string, activeStore *skill.Store, scope skill.Scope) (Candidate, string, error) {
	if s == nil {
		return Candidate{}, "", fmt.Errorf("candidate store is nil")
	}
	if activeStore == nil {
		return Candidate{}, "", fmt.Errorf("active skill store is nil")
	}
	c, err := s.load(hash)
	if err != nil {
		return Candidate{}, "", err
	}
	if err := c.verifyHash(hash); err != nil {
		return Candidate{}, "", err
	}
	switch c.Status {
	case CandidateRejected:
		return Candidate{}, "", fmt.Errorf("candidate %s is rejected: %s", c.Hash, c.ValidationReason)
	case CandidateRolledBack:
		return Candidate{}, "", fmt.Errorf("candidate %s was rolled back: %s", c.Hash, c.ValidationReason)
	case CandidatePromoted:
		return Candidate{}, "", fmt.Errorf("candidate %s is already promoted", c.Hash)
	case CandidatePending, CandidatePromoting:
	default:
		return Candidate{}, "", fmt.Errorf("candidate %s has invalid status %q", c.Hash, c.Status)
	}
	if c.EvalScore == nil {
		return Candidate{}, "", fmt.Errorf("candidate %s has no replay evaluation", c.Hash)
	}
	if c.EvaluatedHash != c.Hash {
		return Candidate{}, "", fmt.Errorf("candidate %s evaluation hash mismatch: %s", c.Hash, c.EvaluatedHash)
	}
	if !c.PromotionGrade {
		return Candidate{}, "", fmt.Errorf("candidate %s has no promotion-grade evaluation", c.Hash)
	}
	if !c.GuardrailPass {
		return Candidate{}, "", fmt.Errorf("candidate %s failed guardrail: %s", c.Hash, c.GuardrailReason)
	}
	if c.Status == CandidatePending {
		c.Status = CandidatePromoting
		c.UpdatedAt = s.now()
		if err := writeJSON(s.path(c.Hash), c); err != nil {
			return Candidate{}, "", err
		}
	}
	path, _, err := eval.Promote(activeStore, c.Skill, scope)
	if err != nil {
		if existing, ok := activeStore.Read(c.Skill.Name); ok && sameSkill(c.Skill, existing) {
			c.Status = CandidatePromoted
			c.PromotedPath = existing.Path
			c.UpdatedAt = s.now()
			if writeErr := writeJSON(s.path(c.Hash), c); writeErr != nil {
				return Candidate{}, "", writeErr
			}
			_ = s.appendAudit("promote_recover", c, existing.Path, "active skill already matched candidate")
			return c, existing.Path, nil
		}
		if c.Status == CandidatePromoting {
			c.Status = CandidatePending
			c.UpdatedAt = s.now()
			_ = writeJSON(s.path(c.Hash), c)
			_ = s.appendAudit("promote_failed", c, "", err.Error())
		}
		return Candidate{}, "", err
	}
	c.Status = CandidatePromoted
	c.PromotedPath = path
	c.UpdatedAt = s.now()
	if err := writeJSON(s.path(c.Hash), c); err != nil {
		return Candidate{}, "", err
	}
	_ = s.appendAudit("promote", c, path, "")
	return c, path, nil
}

func (s *CandidateStore) Rollback(hash string, activeStore *skill.Store, reason string) (Candidate, error) {
	if s == nil {
		return Candidate{}, fmt.Errorf("candidate store is nil")
	}
	if activeStore == nil {
		return Candidate{}, fmt.Errorf("active skill store is nil")
	}
	c, err := s.load(hash)
	if err != nil {
		return Candidate{}, err
	}
	if err := c.verifyHash(hash); err != nil {
		return Candidate{}, err
	}
	if c.Status != CandidatePromoted {
		return Candidate{}, fmt.Errorf("candidate %s is not promoted", c.Hash)
	}
	promotedPath := strings.TrimSpace(c.PromotedPath)
	if promotedPath == "" {
		return Candidate{}, fmt.Errorf("candidate %s has no promoted path", c.Hash)
	}
	existing, ok := activeStore.Read(c.Skill.Name)
	if !ok {
		return Candidate{}, fmt.Errorf("promoted skill %q is not active", c.Skill.Name)
	}
	if filepath.Clean(existing.Path) != filepath.Clean(promotedPath) {
		return Candidate{}, fmt.Errorf("active skill path %q does not match promoted path %q", existing.Path, promotedPath)
	}
	raw, err := os.ReadFile(promotedPath)
	if err != nil {
		return Candidate{}, err
	}
	if strings.TrimSpace(string(raw)) != strings.TrimSpace(eval.RenderSkillMarkdown(c.Skill)) {
		return Candidate{}, fmt.Errorf("promoted skill %q changed since promotion", c.Skill.Name)
	}
	if err := os.Remove(promotedPath); err != nil {
		return Candidate{}, err
	}
	_ = os.Remove(filepath.Dir(promotedPath))
	c.Status = CandidateRolledBack
	c.Validation.Valid = false
	c.Validation.Reason = strings.TrimSpace(reason)
	c.ValidationReason = strings.TrimSpace(reason)
	c.UpdatedAt = s.now()
	if err := writeJSON(s.path(c.Hash), c); err != nil {
		return Candidate{}, err
	}
	_ = s.appendAudit("rollback", c, promotedPath, c.ValidationReason)
	return c, nil
}

func (s *CandidateStore) load(hash string) (Candidate, error) {
	if !isCandidateHash(hash) {
		return Candidate{}, fmt.Errorf("invalid candidate hash %q", hash)
	}
	data, err := os.ReadFile(s.path(hash))
	if err != nil {
		return Candidate{}, err
	}
	var c Candidate
	if err := json.Unmarshal(data, &c); err != nil {
		return Candidate{}, err
	}
	if err := c.verifyHash(hash); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func (c Candidate) verifyHash(expected string) error {
	actual := candidateHash(c.Skill)
	if strings.TrimSpace(c.Hash) != actual {
		return fmt.Errorf("candidate hash mismatch: file has %q, content hashes to %q", c.Hash, actual)
	}
	if strings.TrimSpace(expected) != "" && strings.TrimSpace(expected) != c.Hash {
		return fmt.Errorf("candidate hash mismatch: requested %q, file has %q", expected, c.Hash)
	}
	return nil
}

func (s *CandidateStore) path(hash string) string {
	return filepath.Join(s.Dir, "candidates", hash+".json")
}

func (s *CandidateStore) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func candidateHash(sk skill.Skill) string {
	content := eval.RenderSkillMarkdown(sk)
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func isCandidateHash(hash string) bool {
	hash = strings.TrimSpace(hash)
	if len(hash) != sha256.Size*2 {
		return false
	}
	for _, r := range hash {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func (s *CandidateStore) appendAudit(action string, c Candidate, path, reason string) error {
	if s == nil {
		return fmt.Errorf("candidate store is nil")
	}
	record := AuditRecord{
		Time:   s.now(),
		Action: strings.TrimSpace(action),
		Hash:   c.Hash,
		Name:   c.Skill.Name,
		Status: c.Status,
		Path:   strings.TrimSpace(path),
		Reason: strings.TrimSpace(reason),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func sameSkill(want, got skill.Skill) bool {
	return strings.TrimSpace(want.Name) == strings.TrimSpace(got.Name) &&
		strings.TrimSpace(want.Description) == strings.TrimSpace(got.Description) &&
		strings.TrimSpace(want.Body) == strings.TrimSpace(got.Body)
}

func writeNewJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
