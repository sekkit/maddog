package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"maddog/internal/boot"
	"maddog/internal/config"
	"maddog/internal/provider"
)

type ModelRecommendationRefs struct {
	DefaultModel  string `json:"defaultModel"`
	PlannerModel  string `json:"plannerModel"`
	SubagentModel string `json:"subagentModel"`
	AdvisorModel  string `json:"advisorModel"`
	FrontierModel string `json:"frontierModel"`
}

type modelRecommendationCandidate struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

const modelRecommendationClassifierPrompt = `You classify available coding-agent models into roles.
Return ONLY JSON with these string keys: default_model, planner_model, subagent_model, advisor_model, frontier_model.
Each value must be either "" or one exact ref from the provided candidates.
Role meanings:
- default_model: main loop / everyday coding agent model.
- planner_model: deep planning and architecture reasoning; "" means use default_model.
- subagent_model: small/background/cheap task model; "" means use default_model.
- advisor_model: strong consultation model for hard decisions; "" means use fallback.
- frontier_model: strongest fallback model for repeated failures; "" means no safe recommendation.
Prefer reliable coding/reasoning models for default/planner/advisor/frontier and cheap/fast models for subagent.`

// RecommendModels returns role-specific model recommendations for the Settings
// panel. Known model families are classified locally first; any remaining gaps
// are filled by asking the configured default model to classify the candidates.
func (a *App) RecommendModels() (ModelRecommendationRefs, error) {
	cfg, err := config.Load()
	if err != nil {
		return ModelRecommendationRefs{}, err
	}
	return recommendModelsForConfig(context.Background(), cfg)
}

func recommendModelsForConfig(ctx context.Context, cfg *config.Config) (ModelRecommendationRefs, error) {
	candidates := modelRecommendationCandidatesForConfig(cfg)
	rec := localRecommendedModelRefs(candidates)
	if len(candidates) == 0 {
		return rec, nil
	}
	classifierEntry, ok := modelRecommendationClassifierEntry(cfg, candidates)
	if !ok {
		return rec, nil
	}
	prov, err := boot.NewProviderWithProxy(classifierEntry, cfg.NetworkProxySpec())
	if err != nil {
		return rec, nil
	}
	llmRec, err := classifyModelRecommendations(ctx, prov, candidates)
	if err != nil {
		return rec, nil
	}
	return mergeModelRecommendations(llmRec, rec, candidates), nil
}

func modelRecommendationCandidatesForConfig(cfg *config.Config) []modelRecommendationCandidate {
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	out := []modelRecommendationCandidate{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !modelProviderAccessAllowed(access, p.Name) || !providerSelectableForDesktop(*p) {
			continue
		}
		for _, model := range p.ChatModelList() {
			out = append(out, modelRecommendationCandidate{
				Ref:      p.Name + "/" + model,
				Provider: p.Name,
				Model:    model,
			})
		}
	}
	return out
}

func modelRecommendationClassifierEntry(cfg *config.Config, candidates []modelRecommendationCandidate) (*config.ProviderEntry, bool) {
	candidateRefs := map[string]bool{}
	for _, candidate := range candidates {
		candidateRefs[candidate.Ref] = true
	}
	if entry, ok := cfg.ResolveModel(cfg.DefaultModel); ok && providerSelectableForDesktop(*entry) && candidateRefs[entry.Name+"/"+entry.Model] {
		return entry, true
	}
	for _, candidate := range candidates {
		entry, ok := cfg.ResolveModel(candidate.Ref)
		if ok && providerSelectableForDesktop(*entry) {
			return entry, true
		}
	}
	return nil, false
}

func classifyModelRecommendations(ctx context.Context, prov provider.Provider, candidates []modelRecommendationCandidate) (ModelRecommendationRefs, error) {
	if prov == nil {
		return ModelRecommendationRefs{}, fmt.Errorf("model classifier provider is nil")
	}
	payload, err := json.Marshal(candidates)
	if err != nil {
		return ModelRecommendationRefs{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ch, err := prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: modelRecommendationClassifierPrompt},
			{Role: provider.RoleUser, Content: "CANDIDATES:\n" + string(payload)},
		},
		Temperature: 0,
		MaxTokens:   180,
	})
	if err != nil {
		return ModelRecommendationRefs{}, err
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			return ModelRecommendationRefs{}, chunk.Err
		}
	}
	return parseModelRecommendationJSON(text.String())
}

func parseModelRecommendationJSON(text string) (ModelRecommendationRefs, error) {
	var raw struct {
		DefaultModel  string `json:"default_model"`
		PlannerModel  string `json:"planner_model"`
		SubagentModel string `json:"subagent_model"`
		AdvisorModel  string `json:"advisor_model"`
		FrontierModel string `json:"frontier_model"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &raw); err != nil {
		return ModelRecommendationRefs{}, err
	}
	return ModelRecommendationRefs{
		DefaultModel:  strings.TrimSpace(raw.DefaultModel),
		PlannerModel:  strings.TrimSpace(raw.PlannerModel),
		SubagentModel: strings.TrimSpace(raw.SubagentModel),
		AdvisorModel:  strings.TrimSpace(raw.AdvisorModel),
		FrontierModel: strings.TrimSpace(raw.FrontierModel),
	}, nil
}

func localRecommendedModelRefs(candidates []modelRecommendationCandidate) ModelRecommendationRefs {
	defaultModel := bestLocalModelForRole(candidates, "default")
	plannerModel := bestLocalModelForRole(candidates, "planner")
	subagentModel := bestLocalModelForRole(candidates, "subagent")
	advisorModel := bestKnownLocalModelForRole(candidates, "advisor")
	frontierModel := bestKnownLocalModelForRole(candidates, "frontier")
	return normalizeModelRecommendationRefs(ModelRecommendationRefs{
		DefaultModel:  defaultModel,
		PlannerModel:  plannerModel,
		SubagentModel: subagentModel,
		AdvisorModel:  advisorModel,
		FrontierModel: frontierModel,
	})
}

func bestKnownLocalModelForRole(candidates []modelRecommendationCandidate, role string) string {
	ref, known := bestLocalModelForRoleKnown(candidates, role)
	if !known {
		return ""
	}
	return ref
}

func bestLocalModelForRole(candidates []modelRecommendationCandidate, role string) string {
	ref, _ := bestLocalModelForRoleKnown(candidates, role)
	return ref
}

func bestLocalModelForRoleKnown(candidates []modelRecommendationCandidate, role string) (string, bool) {
	bestRef := ""
	bestScore := 0
	bestKnown := false
	for _, c := range candidates {
		score, known := localModelRoleScore(c, role)
		if score <= 0 {
			continue
		}
		if bestRef == "" || score > bestScore {
			bestRef, bestScore, bestKnown = c.Ref, score, known
		}
	}
	return bestRef, bestKnown
}

func localModelRoleScore(c modelRecommendationCandidate, role string) (int, bool) {
	id := strings.ToLower(c.Provider + "/" + c.Model)
	known := false
	scores := map[string]int{"default": 40, "planner": 30, "subagent": 20, "advisor": 0, "frontier": 0}
	bump := func(delta map[string]int) {
		known = true
		for role, value := range delta {
			scores[role] += value
		}
	}
	if containsAny(id, "gpt-5-codex", "codex", "sonnet", "deepseek-v4-pro", "kimi-k2") || (strings.Contains(id, "qwen") && strings.Contains(id, "coder")) {
		bump(map[string]int{"default": 70, "planner": 55, "advisor": 45, "frontier": 35})
	}
	if containsAny(id, "opus", "grok-4") || (containsAny(id, "o3", "o4", "gemini") && strings.Contains(id, "pro")) || (strings.Contains(id, "qwen") && strings.Contains(id, "max")) {
		bump(map[string]int{"default": 35, "planner": 65, "advisor": 80, "frontier": 90})
	}
	if containsAny(id, "r1", "reason", "thinking") {
		bump(map[string]int{"planner": 55, "advisor": 45, "frontier": 30})
	}
	if containsAny(id, "mini", "nano", "haiku", "flash", "turbo", "lite", "small") {
		bump(map[string]int{"default": 20, "planner": 10, "subagent": 80, "advisor": -5, "frontier": -20})
	}
	if strings.Contains(id, "deepseek-v4-flash") || strings.Contains(id, "local-flash") {
		bump(map[string]int{"default": 35, "planner": 20, "subagent": 85})
	}
	return scores[role], known
}

func containsAny(s string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

func mergeModelRecommendations(primary, fallback ModelRecommendationRefs, candidates []modelRecommendationCandidate) ModelRecommendationRefs {
	allowed := map[string]bool{"": true}
	for _, c := range candidates {
		allowed[c.Ref] = true
	}
	rec := ModelRecommendationRefs{}
	if allowed[primary.DefaultModel] {
		rec.DefaultModel = primary.DefaultModel
	}
	if rec.DefaultModel == "" && allowed[fallback.DefaultModel] {
		rec.DefaultModel = fallback.DefaultModel
	}
	if allowed[primary.PlannerModel] {
		rec.PlannerModel = primary.PlannerModel
	}
	if rec.PlannerModel == "" && allowed[fallback.PlannerModel] {
		rec.PlannerModel = fallback.PlannerModel
	}
	if allowed[primary.SubagentModel] {
		rec.SubagentModel = primary.SubagentModel
	}
	if rec.SubagentModel == "" && allowed[fallback.SubagentModel] {
		rec.SubagentModel = fallback.SubagentModel
	}
	if allowed[primary.AdvisorModel] {
		rec.AdvisorModel = primary.AdvisorModel
	}
	if rec.AdvisorModel == "" && allowed[fallback.AdvisorModel] {
		rec.AdvisorModel = fallback.AdvisorModel
	}
	if allowed[primary.FrontierModel] {
		rec.FrontierModel = primary.FrontierModel
	}
	if rec.FrontierModel == "" && allowed[fallback.FrontierModel] {
		rec.FrontierModel = fallback.FrontierModel
	}
	return normalizeModelRecommendationRefs(rec)
}

func normalizeModelRecommendationRefs(rec ModelRecommendationRefs) ModelRecommendationRefs {
	if rec.PlannerModel == rec.DefaultModel {
		rec.PlannerModel = ""
	}
	if rec.SubagentModel == rec.DefaultModel {
		rec.SubagentModel = ""
	}
	if rec.AdvisorModel == rec.SubagentModel {
		rec.AdvisorModel = ""
	}
	return rec
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end >= start {
		return s[start : end+1]
	}
	return s
}
