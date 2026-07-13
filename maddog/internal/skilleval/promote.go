package skilleval

import (
	"fmt"
	"os"
	"strings"

	"maddog/internal/skill"
)

type PromotionResult struct {
	Path         string
	PromotedHash string
	Created      bool
	Previous     *skill.VersionSnapshot
}

func PromoteSkill(store *skill.Store, sk skill.Skill, scope skill.Scope) (string, error) {
	result, err := PromoteSkillVersioned(store, sk, scope, "")
	return result.Path, err
}

// PromoteSkillVersioned creates a new skill when expectedBaseHash is empty, or
// compare-and-swaps an existing skill and records its immutable prior version.
func PromoteSkillVersioned(store *skill.Store, sk skill.Skill, scope skill.Scope, expectedBaseHash string) (PromotionResult, error) {
	if store == nil {
		return PromotionResult{}, fmt.Errorf("skill store is nil")
	}
	content := RenderSkillMarkdown(sk)
	promotedHash := skill.ContentHash(content)
	existing, exists := store.Read(sk.Name)
	if !exists {
		if strings.TrimSpace(expectedBaseHash) != "" {
			return PromotionResult{}, fmt.Errorf("skill %q base version disappeared before promotion", sk.Name)
		}
		path, err := store.CreateWithContent(sk.Name, scope, content)
		if err != nil {
			return PromotionResult{}, err
		}
		return PromotionResult{Path: path, PromotedHash: promotedHash, Created: true}, nil
	}
	if strings.TrimSpace(expectedBaseHash) == "" {
		return PromotionResult{}, fmt.Errorf("skill %q already exists at %s; versioned promotion requires a base hash", sk.Name, existing.Path)
	}
	snapshot, err := store.ReplaceWithContent(sk.Name, scope, expectedBaseHash, content)
	if err != nil {
		return PromotionResult{}, err
	}
	if _, err := os.Stat(snapshot.SnapshotPath); err != nil {
		return PromotionResult{}, fmt.Errorf("verify previous snapshot for skill %q: %w", sk.Name, err)
	}
	return PromotionResult{
		Path:         snapshot.Path,
		PromotedHash: promotedHash,
		Created:      false,
		Previous:     &snapshot,
	}, nil
}

func RenderSkillMarkdown(sk skill.Skill) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + sk.Name + "\n")
	b.WriteString("description: " + strings.TrimSpace(sk.Description) + "\n")
	if sk.RunAs != "" && sk.RunAs != skill.RunInline {
		b.WriteString("runAs: " + string(sk.RunAs) + "\n")
	}
	if len(sk.AllowedTools) > 0 {
		b.WriteString("allowed-tools: " + strings.Join(sk.AllowedTools, ", ") + "\n")
	}
	if strings.TrimSpace(sk.Model) != "" {
		b.WriteString("model: " + strings.TrimSpace(sk.Model) + "\n")
	}
	if strings.TrimSpace(sk.Effort) != "" {
		b.WriteString("effort: " + strings.TrimSpace(sk.Effort) + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(sk.Body))
	b.WriteString("\n")
	return b.String()
}
