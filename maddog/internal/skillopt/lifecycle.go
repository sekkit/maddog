package skillopt

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"maddog/internal/skill"
)

// PromoteBest explicitly deploys the best completed revision with a base-hash
// compare-and-swap. It is crash-resumable and never runs from Engine.Resume.
func PromoteBest(ctx context.Context, runs RunStore, runID string, skills *skill.Store, scope skill.Scope) (*Run, error) {
	if runs == nil || skills == nil {
		return nil, fmt.Errorf("run and skill stores are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := runs.Load(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != StatusCompleted || !run.Test.Completed {
		return run, fmt.Errorf("run %q is not completed with held-out test evidence", run.ID)
	}
	if run.BestRevisionID == run.BaselineRevisionID {
		return run, fmt.Errorf("run %q has no accepted improvement to promote", run.ID)
	}
	best, ok := run.Revision(run.BestRevisionID)
	if !ok {
		return run, fmt.Errorf("best revision %q is missing", run.BestRevisionID)
	}
	baseline, ok := run.Revision(run.BaselineRevisionID)
	if !ok {
		return run, fmt.Errorf("baseline revision %q is missing", run.BaselineRevisionID)
	}
	active, exists := skills.Read(best.Artifact.Skill.Name)
	if !exists {
		return run, fmt.Errorf("baseline skill %q is no longer active", best.Artifact.Skill.Name)
	}
	raw, err := os.ReadFile(active.Path)
	if err != nil {
		return run, fmt.Errorf("read active skill %q: %w", active.Name, err)
	}
	currentHash := skill.ContentHash(string(raw))
	baseHash := strings.TrimSpace(run.BaselineContentHash)
	content := renderArtifactMarkdown(best.Artifact.Skill)
	promotedHash := skill.ContentHash(content)
	if run.Promotion == nil {
		if baseHash != "" && currentHash != baseHash {
			return run, fmt.Errorf("active skill %q changed since optimization started", active.Name)
		}
		if baseHash == "" && !sameDeployableSkill(baseline.Artifact.Skill, active) {
			return run, fmt.Errorf("active skill %q changed since optimization started", active.Name)
		}
		if baseHash == "" {
			// Compatibility for checkpoints created before the raw baseline hash
			// was recorded. Parsed-field comparison above is the best available
			// guard for those runs.
			baseHash = currentHash
		}
		run.Promotion = &PromotionRecord{
			RevisionID: best.ID, Scope: scope, Path: active.Path,
			BaseHash: baseHash, PromotedHash: promotedHash,
		}
		if err := runs.Save(ctx, run); err != nil {
			return nil, err
		}
	} else {
		promotion := run.Promotion
		if promotion.RevisionID != best.ID || promotion.Scope != scope || promotion.PromotedHash != promotedHash {
			return run, fmt.Errorf("run %q already has different promotion metadata", run.ID)
		}
		if promotion.RolledBack {
			return run, fmt.Errorf("run %q promotion was rolled back", run.ID)
		}
		baseHash = promotion.BaseHash
		if !promotion.PromotedAt.IsZero() {
			return run, nil
		}
		if currentHash != promotion.BaseHash && currentHash != promotion.PromotedHash {
			return run, fmt.Errorf("active skill %q changed during promotion", active.Name)
		}
	}
	snapshot, err := skills.ReplaceWithContent(best.Artifact.Skill.Name, scope, baseHash, content)
	if err != nil {
		return run, err
	}
	run.Promotion.Previous = &snapshot
	run.Promotion.Path = snapshot.Path
	run.Promotion.PromotedAt = time.Now().UTC()
	if err := runs.Save(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// RollbackPromotion restores the exact pre-promotion bytes. A post-promotion
// user edit fails the promoted-hash CAS rather than being overwritten.
func RollbackPromotion(ctx context.Context, runs RunStore, runID string, skills *skill.Store, reason string) (*Run, error) {
	if runs == nil || skills == nil {
		return nil, fmt.Errorf("run and skill stores are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := runs.Load(ctx, runID)
	if err != nil {
		return nil, err
	}
	promotion := run.Promotion
	if promotion == nil || promotion.PromotedAt.IsZero() || promotion.Previous == nil {
		return run, fmt.Errorf("run %q has no completed promotion", run.ID)
	}
	if promotion.RolledBack {
		return run, nil
	}
	active, exists := skills.Read(promotion.Previous.Name)
	if !exists {
		if !promotion.Previous.Existed {
			return markRolledBack(ctx, runs, run, reason)
		}
		return run, fmt.Errorf("promoted skill %q is no longer active", promotion.Previous.Name)
	}
	raw, err := os.ReadFile(active.Path)
	if err != nil {
		return run, err
	}
	currentHash := skill.ContentHash(string(raw))
	if promotion.Previous.Existed && currentHash == promotion.Previous.Hash {
		return markRolledBack(ctx, runs, run, reason)
	}
	if _, err := skills.Restore(promotion.Previous.Name, promotion.Scope, promotion.PromotedHash, *promotion.Previous); err != nil {
		return run, err
	}
	return markRolledBack(ctx, runs, run, reason)
}

func markRolledBack(ctx context.Context, runs RunStore, run *Run, reason string) (*Run, error) {
	now := time.Now().UTC()
	run.Promotion.RolledBack = true
	run.Promotion.RolledBackAt = &now
	run.Promotion.RollbackReason = strings.TrimSpace(reason)
	if err := runs.Save(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func sameDeployableSkill(want, got skill.Skill) bool {
	return strings.TrimSpace(want.Name) == strings.TrimSpace(got.Name) &&
		strings.TrimSpace(want.Description) == strings.TrimSpace(got.Description) &&
		strings.TrimSpace(want.Body) == strings.TrimSpace(got.Body) &&
		want.RunAs == got.RunAs && strings.TrimSpace(want.Model) == strings.TrimSpace(got.Model) &&
		strings.TrimSpace(want.Effort) == strings.TrimSpace(got.Effort) && slices.Equal(want.AllowedTools, got.AllowedTools)
}

func renderArtifactMarkdown(value skill.Skill) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + strings.TrimSpace(value.Name) + "\n")
	b.WriteString("description: " + strings.TrimSpace(value.Description) + "\n")
	if value.RunAs != "" && value.RunAs != skill.RunInline {
		b.WriteString("runAs: " + string(value.RunAs) + "\n")
	}
	if len(value.AllowedTools) > 0 {
		b.WriteString("allowed-tools: " + strings.Join(value.AllowedTools, ", ") + "\n")
	}
	if strings.TrimSpace(value.Model) != "" {
		b.WriteString("model: " + strings.TrimSpace(value.Model) + "\n")
	}
	if strings.TrimSpace(value.Effort) != "" {
		b.WriteString("effort: " + strings.TrimSpace(value.Effort) + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(value.Body))
	b.WriteString("\n")
	return b.String()
}
