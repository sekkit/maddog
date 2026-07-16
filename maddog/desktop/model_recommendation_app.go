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

const modelRecommendationClassifierPrompt = `You are selecting role-specific models for a coding agent's Settings panel.
Return ONLY one JSON object with exactly these string keys: default_model, planner_model, subagent_model, advisor_model, frontier_model.
Every non-empty value must be one exact ref from the provided candidates. Do not include explanations, markdown, or invented model names.

The user explicitly prefers the GPT family. GPT-5.6 is the first choice for coding quality and value, but its variants are not interchangeable. Follow these rules exactly:
1. The verified high-capability GPT-5.6 tier is: gpt-5.6, gpt-5.6-sol, and gpt-5.6-terra. Prefer this tier for default_model, planner_model, advisor_model, and frontier_model over non-GPT families when available.
2. gpt-5.6-luna is the lowest-priority GPT-5.6 variant. It MUST NEVER be selected for advisor_model or frontier_model.
3. If any verified high-capability GPT-5.6 candidate exists, gpt-5.6-luna MUST NOT be selected for default_model or planner_model. It may be considered only for subagent_model.
4. Do not infer a price or capability difference between gpt-5.6-sol and gpt-5.6-terra merely from their suffix. Treat them as the same preferred tier unless the candidate name provides an explicit signal. Treat an unlisted gpt-5.6-* variant as unknown instead of promoting it automatically.
5. For subagent_model, prefer an explicitly small/fast model (mini, flash, lite, haiku) when that is a good cost/latency fit. If no such choice is clearly appropriate, return "" to use the default model rather than guessing.
6. Planner should normally return "" when the default model is already suitable. Advisor and frontier should be strong, reliable reasoning/coding models; return "" rather than using a weak or uncertain model.
7. Keep the recommendation stable and simple: do not spread roles across models without a clear role-specific reason.

Role meanings:
- default_model: main loop / everyday coding agent model.
- planner_model: deep planning and architecture reasoning; "" means use default_model.
- subagent_model: small/background/cheap task model; "" means use default_model.
- advisor_model: strong consultation model for hard decisions; "" means use fallback.
- frontier_model: strongest fallback model for repeated failures; "" means no safe recommendation.`

// newModelRecommendationProvider is replaceable in tests so the recommendation
// policy can be exercised without making a network request.
var newModelRecommendationProvider = boot.NewProviderWithProxy

// RecommendModels returns role-specific model recommendations for the Settings
// panel. It reads the same persistent user configuration that the Settings panel
// edits, then sends an explicit GPT-5.6 variant policy to a capable classifier.
func (a *App) RecommendModels() (ModelRecommendationRefs, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
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
	// Ask a capable configured model to make the final role split with the
	// explicit policy prompt above. The local recommendation is still retained as
	// a safe fallback and is used to enforce non-negotiable variant constraints.
	classifierEntry, ok := modelRecommendationClassifierEntry(cfg, candidates)
	if !ok {
		return rec, nil
	}
	prov, err := newModelRecommendationProvider(classifierEntry, cfg.NetworkProxySpec())
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
	// Do not ask a low-tier luna deployment to decide the role assignments when
	// a normal GPT candidate is already configured. GPT-5.6 (except luna) wins,
	// followed by the remaining supported GPT generations.
	if ref := bestGPTRecommendationClassifierRef(candidates); ref != "" {
		if entry, ok := cfg.ResolveModel(ref); ok && providerSelectableForDesktop(*entry) {
			return entry, true
		}
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

func bestGPTRecommendationClassifierRef(candidates []modelRecommendationCandidate) string {
	bestRef := ""
	bestPriority := 0
	for _, candidate := range candidates {
		priority := gptFamilyPriority(candidate.Model)
		if priority > bestPriority {
			bestRef, bestPriority = candidate.Ref, priority
		}
	}
	return bestRef
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
		Temperature: provider.TemperaturePtr(0),
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
	modelID := strings.ToLower(strings.TrimSpace(c.Model))
	known := false
	scores := map[string]int{"default": 40, "planner": 30, "subagent": 20, "advisor": 0, "frontier": 0}
	bump := func(delta map[string]int) {
		known = true
		for role, value := range delta {
			scores[role] += value
		}
	}
	if isGPT56Luna(modelID) {
		// luna is a lower-tier GPT-5.6 variant. It can be considered for isolated
		// background work, but it must never become a high-stakes recommendation.
		bump(map[string]int{
			"default":  -20,
			"planner":  -30,
			"subagent": 55,
			"advisor":  -1000,
			"frontier": -1000,
		})
	} else if priority := gptFamilyPriority(modelID); priority > 0 {
		// Keep normal GPT-5.6 variants at the top tier for high-capability work.
		bump(map[string]int{
			"default":  priority,
			"planner":  priority,
			"subagent": priority,
			"advisor":  priority,
			"frontier": priority,
		})
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

func gptFamilyPriority(id string) int {
	id = strings.ToLower(strings.TrimSpace(id))
	switch {
	case isGPT56Luna(id):
		return 0
	case isVerifiedHighCapabilityGPT56(id):
		return 1000
	case strings.Contains(id, "gpt-5.6"):
		// An unknown 5.6 suffix must not silently inherit sol/terra's priority.
		return 500
	case strings.Contains(id, "gpt-5.5"):
		return 850
	case strings.Contains(id, "gpt-5.4") && !containsAny(id, "mini", "nano"):
		return 700
	case strings.Contains(id, "gpt-5.4"):
		return 550
	case strings.Contains(id, "gpt-5"):
		return 500
	default:
		return 0
	}
}

func isVerifiedHighCapabilityGPT56(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra":
		return true
	default:
		return false
	}
}

func isGPT56Luna(id string) bool {
	return strings.Contains(strings.ToLower(id), "gpt-5.6-luna")
}

func hasNonLunaGPTCandidate(candidates []modelRecommendationCandidate) bool {
	for _, candidate := range candidates {
		if gptFamilyPriority(candidate.Model) > 0 {
			return true
		}
	}
	return false
}

func modelRecommendationRefAllowedForRole(ref, role string, candidates []modelRecommendationCandidate) bool {
	if ref == "" {
		return true
	}
	for _, candidate := range candidates {
		if candidate.Ref != ref {
			continue
		}
		if !isGPT56Luna(candidate.Model) {
			return true
		}
		switch role {
		case "advisor", "frontier":
			return false
		case "default", "planner":
			return !hasNonLunaGPTCandidate(candidates)
		default:
			return true // luna is permitted only as an optional background/subagent choice.
		}
	}
	return false
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
	choose := func(primaryRef, fallbackRef, role string) string {
		if modelRecommendationRefAllowedForRole(primaryRef, role, candidates) && primaryRef != "" {
			return primaryRef
		}
		if modelRecommendationRefAllowedForRole(fallbackRef, role, candidates) {
			return fallbackRef
		}
		return ""
	}
	rec := ModelRecommendationRefs{
		DefaultModel:  choose(primary.DefaultModel, fallback.DefaultModel, "default"),
		PlannerModel:  choose(primary.PlannerModel, fallback.PlannerModel, "planner"),
		SubagentModel: choose(primary.SubagentModel, fallback.SubagentModel, "subagent"),
		AdvisorModel:  choose(primary.AdvisorModel, fallback.AdvisorModel, "advisor"),
		FrontierModel: choose(primary.FrontierModel, fallback.FrontierModel, "frontier"),
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
