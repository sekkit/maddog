package skilleval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"maddog/internal/eval"
	"maddog/internal/fileutil"
	"maddog/internal/skill"
)

type CandidateStatus string

const (
	CandidatePending   CandidateStatus = "pending"
	CandidatePromoting CandidateStatus = "promoting"
	CandidateRejected  CandidateStatus = "rejected"
	CandidatePromoted  CandidateStatus = "promoted"
)

type CandidateStore struct {
	Dir string
	Now func() time.Time
}

type Candidate struct {
	Hash             string          `json:"hash"`
	Skill            skill.Skill     `json:"skill"`
	Status           CandidateStatus `json:"status"`
	SourceBundleID   string          `json:"source_bundle_id,omitempty"`
	SourceBundlePath string          `json:"source_bundle_path,omitempty"`
	SourceTask       string          `json:"source_task,omitempty"`
	Validation       ValidationInfo  `json:"validation"`
	ValidationReason string          `json:"validation_reason,omitempty"`
	EvalScore        *ScoreResult    `json:"eval_score,omitempty"`
	EvaluatedHash    string          `json:"evaluated_hash,omitempty"`
	GuardrailPass    bool            `json:"guardrail_pass,omitempty"`
	GuardrailReason  string          `json:"guardrail_reason,omitempty"`
	PromotedPath     string          `json:"promoted_path,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type ValidationInfo struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

func NewCandidateStore(dir string) *CandidateStore {
	return &CandidateStore{Dir: dir, Now: time.Now}
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

func (s *CandidateStore) RecordEvaluation(hash string, score ScoreResult, guardrail GuardrailResult) (Candidate, error) {
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
			return c, existing.Path, nil
		}
		return Candidate{}, "", err
	}
	c.Status = CandidatePromoted
	c.PromotedPath = path
	c.UpdatedAt = s.now()
	if err := writeJSON(s.path(c.Hash), c); err != nil {
		return Candidate{}, "", err
	}
	return c, path, nil
}

func (s *CandidateStore) load(hash string) (Candidate, error) {
	data, err := os.ReadFile(s.path(hash))
	if err != nil {
		return Candidate{}, err
	}
	var c Candidate
	if err := json.Unmarshal(data, &c); err != nil {
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
