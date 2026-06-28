package skill

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/event"
)

type Orchestrator struct {
	Store         *Store
	Matcher       Matcher
	Generator     Generator
	Validator     Validator
	DynamicSkills bool
}

type OrchestrationResult struct {
	Skill     Skill
	Matched   bool
	Generated bool
	Prompt    string
	Reason    string
}

func NewOrchestrator(store *Store, generator Generator) *Orchestrator {
	return &Orchestrator{
		Store:         store,
		Matcher:       NewMatcher(store),
		Generator:     generator,
		Validator:     NewValidator(),
		DynamicSkills: generator.prov != nil,
	}
}

func (o *Orchestrator) Orchestrate(ctx context.Context, task string) (OrchestrationResult, error) {
	if o == nil || o.Store == nil {
		return OrchestrationResult{}, nil
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return OrchestrationResult{}, nil
	}
	validator := o.Validator
	if validator.bodyLimit == 0 {
		validator = NewValidator()
	}
	if validator.IsHighRisk(task) {
		return OrchestrationResult{Reason: "high-risk task; skipped dynamic skill generation"}, nil
	}
	matcher := o.Matcher
	if matcher.store == nil {
		matcher = NewMatcher(o.Store)
	}
	if match := matcher.Match(task); match.Matched {
		return OrchestrationResult{
			Skill:   match.Skill,
			Matched: true,
			Prompt:  orchestrationPrompt(match.Skill, false),
			Reason:  match.Reason,
		}, nil
	}
	if !o.DynamicSkills {
		return OrchestrationResult{}, nil
	}
	sk, err := o.Generator.Generate(ctx, task)
	if err != nil {
		return OrchestrationResult{}, err
	}
	if res := validator.Validate(sk, task); !res.Valid {
		return OrchestrationResult{}, fmt.Errorf("dynamic skill rejected: %s", res.Reason)
	}
	if err := o.Store.Inject(sk); err != nil {
		return OrchestrationResult{}, err
	}
	return OrchestrationResult{
		Skill:     sk,
		Generated: true,
		Prompt:    orchestrationPrompt(sk, true),
		Reason:    "generated dynamic skill " + sk.Name,
	}, nil
}

func (r OrchestrationResult) Event() event.Event {
	kind := event.Notice
	text := r.Reason
	if r.Generated {
		kind = event.SkillGenerated
		text = "generated dynamic skill " + r.Skill.Name
	}
	return event.Event{Kind: kind, Level: event.LevelInfo, Text: text}
}

func orchestrationPrompt(sk Skill, generated bool) string {
	prefix := "Runtime skill orchestration matched an existing skill"
	if generated {
		prefix = "Runtime skill orchestration generated a temporary dynamic skill"
	}
	return fmt.Sprintf("<runtime-skill>\n%s: %s — %s.\nInvoke it with run_skill name=%q if it is relevant before continuing. This is a turn-local hint, not a system prompt override.\n</runtime-skill>",
		prefix, sk.Name, sk.Description, sk.Name)
}
