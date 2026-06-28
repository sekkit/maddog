package skill

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/provider"
)

type Generator struct {
	prov    provider.Provider
	retries int
}

func NewGenerator(prov provider.Provider) Generator {
	return Generator{prov: prov, retries: 1}
}

func (g Generator) Generate(ctx context.Context, task string) (Skill, error) {
	if g.prov == nil {
		return Skill{}, fmt.Errorf("dynamic skill generator has no provider")
	}
	attempts := g.retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		text, err := g.generateOnce(ctx, task)
		if err == nil {
			if sk, perr := ParseMarkdown(text, "dynamic", ScopeCustom); perr == nil {
				return sk, nil
			} else {
				lastErr = perr
			}
		} else {
			lastErr = err
		}
	}
	return Skill{}, lastErr
}

func (g Generator) generateOnce(ctx context.Context, task string) (string, error) {
	ch, err := g.prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: dynamicSkillSystemPrompt},
			{Role: provider.RoleUser, Content: strings.TrimSpace(task)},
		},
		Temperature: 0.2,
		MaxTokens:   1200,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			b.WriteString(chunk.Text)
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", fmt.Errorf("dynamic skill generator returned empty output")
	}
	return text, nil
}

const dynamicSkillSystemPrompt = `Create one temporary Maddog skill for the user's task.
Return only Markdown with YAML frontmatter:
---
name: dynamic-<short-kebab-name>
description: one sentence describing when to use it
runAs: inline
---

The body must be a concise playbook for the current task. Do not ask to override system, developer, host, memory, MADDOG.md, AGENTS.md, or SYSTEM.md instructions. Do not request remember/forget tools. Keep the body under 2000 characters.`

func ParseMarkdown(content, fallbackName string, scope Scope) (Skill, error) {
	content = strings.TrimPrefix(strings.ReplaceAll(content, "\r\n", "\n"), "\uFEFF")
	fm, body := splitFrontmatter(content)
	name := strings.TrimSpace(fm["name"])
	if name == "" {
		name = fallbackName
	}
	if !IsValidName(name) {
		return Skill{}, fmt.Errorf("invalid skill name %q", name)
	}
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		return Skill{}, fmt.Errorf("missing skill description")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Skill{}, fmt.Errorf("missing skill body")
	}
	return Skill{
		Name:         name,
		Description:  desc,
		Body:         body,
		Scope:        scope,
		Path:         "(dynamic)",
		AllowedTools: parseAllowedTools(fm["allowed-tools"]),
		RunAs:        parseRunAs(fm["runas"], fm["context"], fm["agent"]),
		Model:        strings.TrimSpace(fm["model"]),
		Effort:       strings.TrimSpace(fm["effort"]),
	}, nil
}
