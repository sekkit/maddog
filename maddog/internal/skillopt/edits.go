package skillopt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

	"maddog/internal/skill"
)

func newArtifact(value skill.Skill) (Artifact, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return Artifact{}, fmt.Errorf("marshal skill artifact: %w", err)
	}
	digest := sha256.Sum256(data)
	return Artifact{Skill: cloneSkill(value), Digest: hex.EncodeToString(digest[:])}, nil
}

func cloneSkill(value skill.Skill) skill.Skill {
	value.AllowedTools = append([]string(nil), value.AllowedTools...)
	return value
}

func sameFrozenFields(base, candidate skill.Skill) bool {
	return base.Name == candidate.Name &&
		base.Description == candidate.Description &&
		base.Scope == candidate.Scope &&
		base.Path == candidate.Path &&
		slices.Equal(base.AllowedTools, candidate.AllowedTools) &&
		base.RunAs == candidate.RunAs &&
		base.Model == candidate.Model &&
		base.Effort == candidate.Effort
}

func validateAndApplyProposal(base skill.Skill, proposal Proposal, limits EditLimits) (skill.Skill, error) {
	if !sameFrozenFields(base, proposal.Candidate) {
		return skill.Skill{}, ErrCapabilityMutation
	}
	if len(proposal.Edits) == 0 {
		return skill.Skill{}, fmt.Errorf("%w: proposal has no body edits", ErrInvalidProposal)
	}
	if len(proposal.Edits) > limits.MaxEdits {
		return skill.Skill{}, fmt.Errorf("%w: %d edits exceed limit %d", ErrInvalidProposal, len(proposal.Edits), limits.MaxEdits)
	}

	changed := 0
	previousEnd := 0
	for i, edit := range proposal.Edits {
		if edit.Start < 0 || edit.End < edit.Start || edit.End > len(base.Body) {
			return skill.Skill{}, fmt.Errorf("%w: edit %d range [%d,%d) is invalid", ErrInvalidProposal, i, edit.Start, edit.End)
		}
		if i > 0 && edit.Start < previousEnd {
			return skill.Skill{}, fmt.Errorf("%w: edits are unordered or overlapping", ErrInvalidProposal)
		}
		if !utf8.ValidString(base.Body[:edit.Start]) || !utf8.ValidString(base.Body[:edit.End]) {
			return skill.Skill{}, fmt.Errorf("%w: edit %d splits a UTF-8 sequence", ErrInvalidProposal, i)
		}
		changed += edit.End - edit.Start + len(edit.Replacement)
		previousEnd = edit.End
	}
	if changed > limits.MaxChangedBytes {
		return skill.Skill{}, fmt.Errorf("%w: %d changed bytes exceed limit %d", ErrInvalidProposal, changed, limits.MaxChangedBytes)
	}

	body := base.Body
	for i := len(proposal.Edits) - 1; i >= 0; i-- {
		edit := proposal.Edits[i]
		body = body[:edit.Start] + edit.Replacement + body[edit.End:]
	}
	if len(body) > limits.MaxBodyBytes {
		return skill.Skill{}, fmt.Errorf("%w: body size %d exceeds limit %d", ErrInvalidProposal, len(body), limits.MaxBodyBytes)
	}
	if !utf8.ValidString(body) {
		return skill.Skill{}, fmt.Errorf("%w: resulting body is not valid UTF-8", ErrInvalidProposal)
	}
	if proposal.Candidate.Body != body {
		return skill.Skill{}, fmt.Errorf("%w: candidate body does not match structured edits", ErrInvalidProposal)
	}
	return cloneSkill(proposal.Candidate), nil
}

func validateResult(result Result) error {
	if math.IsNaN(result.Soft) || math.IsInf(result.Soft, 0) || result.Soft < 0 || result.Soft > 1 {
		return fmt.Errorf("rollout soft score must be in [0,1]")
	}
	return validateCost(result.Cost)
}

func validateCost(cost Cost) error {
	if cost.InputTokens < 0 || cost.OutputTokens < 0 || cost.Amount < 0 || math.IsNaN(cost.Amount) || math.IsInf(cost.Amount, 0) {
		return fmt.Errorf("operation returned invalid cost")
	}
	return nil
}
