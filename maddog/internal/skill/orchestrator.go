package skill

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/skilleval"
)

type OrchestrationResult struct {
	Matched         bool
	Generated       bool
	Skill           Skill
	Prompt          string
	Notice          string
	BundleID        string
	CandidateID     string
	CandidateStatus string
}

type Orchestrator struct {
	store          *Store
	matcher        Matcher
	generator      Generator
	validator      Validator
	candidateStore *skilleval.Store
}

func NewOrchestrator(store *Store, generator Generator) Orchestrator {
	var candidates *skilleval.Store
	if store != nil && store.ProjectRoot() != "" {
		candidates = skilleval.NewProjectStore(store.ProjectRoot())
	}
	return Orchestrator{
		store:          store,
		matcher:        NewMatcher(store),
		generator:      generator,
		validator:      NewValidator(),
		candidateStore: candidates,
	}
}

func (o Orchestrator) WithCandidateStore(store *skilleval.Store) Orchestrator {
	o.candidateStore = store
	return o
}

func (o Orchestrator) Orchestrate(ctx context.Context, task string) (OrchestrationResult, error) {
	task = strings.TrimSpace(task)
	if task == "" || o.store == nil {
		return OrchestrationResult{}, nil
	}
	if match := o.matcher.Match(task); match.Matched {
		return OrchestrationResult{
			Matched: true,
			Skill:   match.Skill,
			Prompt:  runtimeSkillPrompt(match.Skill),
			Notice:  "matched existing skill " + match.Skill.Name,
		}, nil
	}
	if o.validator.IsHighRisk(task) {
		return OrchestrationResult{}, nil
	}
	sk, err := o.generator.Generate(ctx, task)
	if err != nil {
		return OrchestrationResult{}, err
	}
	verdict := o.validator.Validate(sk, task)
	record, err := o.recordCandidate(task, sk, verdict)
	if err != nil {
		return OrchestrationResult{}, err
	}
	status := string(skilleval.CandidatePending)
	if !verdict.Valid {
		status = string(skilleval.CandidateRejected)
	}
	notice := "generated pending skill candidate " + sk.Name
	if !verdict.Valid {
		notice = fmt.Sprintf("rejected dynamic skill candidate %s: %s", sk.Name, verdict.Reason)
	}
	return OrchestrationResult{
		Generated:       true,
		Skill:           sk,
		Notice:          notice,
		BundleID:        record.Bundle.ID,
		CandidateID:     record.Candidate.ID,
		CandidateStatus: status,
	}, nil
}

func (o Orchestrator) recordCandidate(task string, sk Skill, verdict ValidationResult) (skilleval.GeneratedSkillResult, error) {
	if o.candidateStore == nil {
		return skilleval.GeneratedSkillResult{}, fmt.Errorf("dynamic skill candidate store is unavailable")
	}
	return o.candidateStore.RecordGeneratedSkill(skilleval.GeneratedSkillRecord{
		Task:   task,
		Source: "runtime_skill_generation",
		Snapshot: map[string]any{
			"skill_name": sk.Name,
			"run_as":     string(sk.RunAs),
		},
		Skill: skilleval.SkillSnapshot{
			Name:         sk.Name,
			Description:  sk.Description,
			Body:         sk.Body,
			AllowedTools: sk.AllowedTools,
			RunAs:        string(sk.RunAs),
			Model:        sk.Model,
			Effort:       sk.Effort,
		},
		Validation: skilleval.ValidationSnapshot{Valid: verdict.Valid, Reason: verdict.Reason},
	})
}

func runtimeSkillPrompt(sk Skill) string {
	return fmt.Sprintf(`<runtime-skill>
<skill name="%s">
Use run_skill with this skill when it helps the current task. The skill is available in the runtime skill store.
</skill>
</runtime-skill>`, sk.Name)
}
