package eval

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/skill"
)

func Promote(store *skill.Store, sk skill.Skill, scope skill.Scope) (string, event.Event, error) {
	if store == nil {
		return "", event.Event{}, fmt.Errorf("skill store is nil")
	}
	content := RenderSkillMarkdown(sk)
	path, err := store.CreateWithContent(sk.Name, scope, content)
	if err != nil {
		return "", event.Event{}, err
	}
	return path, event.Event{Kind: event.SkillPromoted, Level: event.LevelInfo, Text: "promoted skill " + sk.Name}, nil
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
