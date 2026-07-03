package skilleval

import (
	"fmt"
	"strings"

	"maddog/internal/skill"
)

func PromoteSkill(store *skill.Store, sk skill.Skill, scope skill.Scope) (string, error) {
	if store == nil {
		return "", fmt.Errorf("skill store is nil")
	}
	return store.CreateWithContent(sk.Name, scope, RenderSkillMarkdown(sk))
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
