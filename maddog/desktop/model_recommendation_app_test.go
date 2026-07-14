package main

import (
	"context"
	"strings"
	"testing"

	"maddog/internal/config"
	"maddog/internal/netclient"
	"maddog/internal/provider"
)

type modelRecommendationProvider struct {
	text string
	req  provider.Request
}

func (p *modelRecommendationProvider) Name() string { return "classifier" }

func (p *modelRecommendationProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.req = req
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.text}
	close(ch)
	return ch, nil
}

func TestParseModelRecommendationJSON(t *testing.T) {
	got, err := parseModelRecommendationJSON("```json\n{\"default_model\":\"local/a\",\"planner_model\":\"\",\"subagent_model\":\"local/b\",\"advisor_model\":\"local/c\",\"frontier_model\":\"local/c\"}\n```")
	if err != nil {
		t.Fatalf("parseModelRecommendationJSON: %v", err)
	}
	want := ModelRecommendationRefs{
		DefaultModel:  "local/a",
		SubagentModel: "local/b",
		AdvisorModel:  "local/c",
		FrontierModel: "local/c",
	}
	if got != want {
		t.Fatalf("recommendations = %+v, want %+v", got, want)
	}
}

func TestClassifyModelRecommendationsUsesProvider(t *testing.T) {
	p := &modelRecommendationProvider{text: `{"default_model":"unknown/alpha","planner_model":"","subagent_model":"unknown/beta","advisor_model":"unknown/gamma","frontier_model":"unknown/gamma"}`}
	candidates := []modelRecommendationCandidate{
		{Ref: "unknown/alpha", Provider: "unknown", Model: "alpha"},
		{Ref: "unknown/beta", Provider: "unknown", Model: "beta"},
		{Ref: "unknown/gamma", Provider: "unknown", Model: "gamma"},
	}

	got, err := classifyModelRecommendations(context.Background(), p, candidates)
	if err != nil {
		t.Fatalf("classifyModelRecommendations: %v", err)
	}
	if got.AdvisorModel != "unknown/gamma" || got.FrontierModel != "unknown/gamma" {
		t.Fatalf("LLM recommendations = %+v", got)
	}
	if len(p.req.Messages) != 2 || p.req.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("request messages = %+v", p.req.Messages)
	}
	if p.req.Temperature != 0 || p.req.MaxTokens != 180 {
		t.Fatalf("request limits = temp %v max %d", p.req.Temperature, p.req.MaxTokens)
	}
}

func TestModelRecommendationClassifierEntryPrefersDefaultUsableModel(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "preferred/gamma",
		Desktop:      config.DesktopConfig{ProviderAccess: []string{"fallback", "preferred"}},
		Providers: []config.ProviderEntry{
			{Name: "fallback", Kind: "openai", BaseURL: "http://localhost", Models: []string{"alpha"}},
			{Name: "preferred", Kind: "openai", BaseURL: "http://localhost", Models: []string{"beta", "gamma"}, Default: "beta"},
		},
	}
	candidates := modelRecommendationCandidatesForConfig(cfg)

	got, ok := modelRecommendationClassifierEntry(cfg, candidates)
	if !ok {
		t.Fatal("modelRecommendationClassifierEntry ok = false")
	}
	if got.Name != "preferred" || got.Model != "gamma" {
		t.Fatalf("classifier entry = %s/%s, want preferred/gamma", got.Name, got.Model)
	}
}

func TestModelRecommendationClassifierEntryFallsBackToFirstUsableCandidate(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "hidden/gamma",
		Desktop:      config.DesktopConfig{ProviderAccess: []string{"fallback"}},
		Providers: []config.ProviderEntry{
			{Name: "hidden", Kind: "openai", BaseURL: "http://localhost", Models: []string{"gamma"}},
			{Name: "fallback", Kind: "openai", BaseURL: "http://localhost", Models: []string{"alpha"}},
		},
	}
	candidates := modelRecommendationCandidatesForConfig(cfg)

	got, ok := modelRecommendationClassifierEntry(cfg, candidates)
	if !ok {
		t.Fatal("modelRecommendationClassifierEntry ok = false")
	}
	if got.Name != "fallback" || got.Model != "alpha" {
		t.Fatalf("classifier entry = %s/%s, want fallback/alpha", got.Name, got.Model)
	}
}

func TestMergeModelRecommendationsPrefersValidClassifierAndFallsBack(t *testing.T) {
	classifier := ModelRecommendationRefs{
		DefaultModel:  "evil/not-a-candidate",
		SubagentModel: "local/local-flash",
		AdvisorModel:  "mystery/mystery-pro",
		FrontierModel: "mystery/mystery-pro",
	}
	fallback := ModelRecommendationRefs{
		DefaultModel:  "local/local-flash",
		PlannerModel:  "local/local-flash",
		SubagentModel: "mystery/mystery-pro",
		AdvisorModel:  "local/local-flash",
		FrontierModel: "local/local-flash",
	}
	candidates := []modelRecommendationCandidate{
		{Ref: "local/local-flash", Provider: "local", Model: "local-flash"},
		{Ref: "mystery/mystery-pro", Provider: "mystery", Model: "mystery-pro"},
	}

	got := mergeModelRecommendations(classifier, fallback, candidates)
	want := ModelRecommendationRefs{
		DefaultModel:  "local/local-flash",
		AdvisorModel:  "mystery/mystery-pro",
		FrontierModel: "mystery/mystery-pro",
	}
	if got != want {
		t.Fatalf("merged recommendations = %+v, want %+v", got, want)
	}
}

func TestLocalRecommendationsPreferNonLunaGPT56ForHighValueRoles(t *testing.T) {
	candidates := []modelRecommendationCandidate{
		{Ref: "ice/gpt-5.6-luna", Provider: "ice", Model: "gpt-5.6-luna"},
		{Ref: "ice/gpt-5.6-sol", Provider: "ice", Model: "gpt-5.6-sol"},
		{Ref: "ice/gpt-5.6-terra", Provider: "ice", Model: "gpt-5.6-terra"},
		{Ref: "ice/gpt-5.5", Provider: "ice", Model: "gpt-5.5"},
		{Ref: "ice/gpt-5.4-mini", Provider: "ice", Model: "gpt-5.4-mini"},
		{Ref: "deepseek/deepseek-v4-pro", Provider: "deepseek", Model: "deepseek-v4-pro"},
		{Ref: "ice/claude-opus-4-6", Provider: "ice", Model: "claude-opus-4-6"},
	}

	got := localRecommendedModelRefs(candidates)
	want := ModelRecommendationRefs{
		DefaultModel:  "ice/gpt-5.6-sol",
		AdvisorModel:  "ice/gpt-5.6-sol",
		FrontierModel: "ice/gpt-5.6-sol",
	}
	if got != want {
		t.Fatalf("recommendations = %+v, want %+v", got, want)
	}
}

func TestModelRecommendationClassifierEntryPrefersNonLunaGPT56(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "ice/gpt-5.6-luna",
		Desktop:      config.DesktopConfig{ProviderAccess: []string{"ice"}},
		Providers: []config.ProviderEntry{
			{Name: "ice", Kind: "openai", BaseURL: "http://localhost", Models: []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"}},
		},
	}
	candidates := modelRecommendationCandidatesForConfig(cfg)

	got, ok := modelRecommendationClassifierEntry(cfg, candidates)
	if !ok {
		t.Fatal("modelRecommendationClassifierEntry ok = false")
	}
	if got.Name != "ice" || got.Model != "gpt-5.6-sol" {
		t.Fatalf("classifier entry = %s/%s, want ice/gpt-5.6-sol", got.Name, got.Model)
	}
}

func TestMergeModelRecommendationsRejectsLunaForHighValueRoles(t *testing.T) {
	primary := ModelRecommendationRefs{
		DefaultModel:  "ice/gpt-5.6-luna",
		PlannerModel:  "ice/gpt-5.6-luna",
		SubagentModel: "ice/gpt-5.6-luna",
		AdvisorModel:  "ice/gpt-5.6-luna",
		FrontierModel: "ice/gpt-5.6-luna",
	}
	fallback := ModelRecommendationRefs{
		DefaultModel:  "ice/gpt-5.6-sol",
		PlannerModel:  "ice/gpt-5.6-sol",
		AdvisorModel:  "ice/gpt-5.6-sol",
		FrontierModel: "ice/gpt-5.6-sol",
	}
	candidates := []modelRecommendationCandidate{
		{Ref: "ice/gpt-5.6-luna", Provider: "ice", Model: "gpt-5.6-luna"},
		{Ref: "ice/gpt-5.6-sol", Provider: "ice", Model: "gpt-5.6-sol"},
	}

	got := mergeModelRecommendations(primary, fallback, candidates)
	want := ModelRecommendationRefs{
		DefaultModel:  "ice/gpt-5.6-sol",
		SubagentModel: "ice/gpt-5.6-luna",
		AdvisorModel:  "ice/gpt-5.6-sol",
		FrontierModel: "ice/gpt-5.6-sol",
	}
	if got != want {
		t.Fatalf("merged recommendations = %+v, want %+v", got, want)
	}
}

func TestRecommendModelsUsesPolicyPromptAndLunaGuardrails(t *testing.T) {
	t.Setenv("ICE_RECOMMEND_TEST_KEY", "test-key")
	cfg := &config.Config{
		DefaultModel: "ice/gpt-5.6-luna",
		Desktop:      config.DesktopConfig{ProviderAccess: []string{"ice"}},
		Providers: []config.ProviderEntry{
			{
				Name:      "ice",
				Kind:      "openai",
				BaseURL:   "https://example.invalid",
				Models:    []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.5", "deepseek-v4-flash"},
				APIKeyEnv: "ICE_RECOMMEND_TEST_KEY",
			},
		},
	}

	originalFactory := newModelRecommendationProvider
	classifier := &modelRecommendationProvider{text: `{"default_model":"ice/gpt-5.6-luna","planner_model":"ice/gpt-5.6-luna","subagent_model":"ice/gpt-5.6-luna","advisor_model":"ice/gpt-5.6-luna","frontier_model":"ice/gpt-5.6-luna"}`}
	newModelRecommendationProvider = func(entry *config.ProviderEntry, _ netclient.ProxySpec) (provider.Provider, error) {
		if entry.Name != "ice" || entry.Model != "gpt-5.6-sol" {
			t.Fatalf("classifier model = %s/%s, want ice/gpt-5.6-sol", entry.Name, entry.Model)
		}
		return classifier, nil
	}
	t.Cleanup(func() { newModelRecommendationProvider = originalFactory })

	got, err := recommendModelsForConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("recommendModelsForConfig: %v", err)
	}
	want := ModelRecommendationRefs{
		DefaultModel:  "ice/gpt-5.6-sol",
		SubagentModel: "ice/gpt-5.6-luna",
		AdvisorModel:  "ice/gpt-5.6-sol",
		FrontierModel: "ice/gpt-5.6-sol",
	}
	if got != want {
		t.Fatalf("recommendations = %+v, want %+v", got, want)
	}
	if len(classifier.req.Messages) != 2 || !strings.Contains(classifier.req.Messages[0].Content, "MUST NEVER be selected for advisor_model") || !strings.Contains(classifier.req.Messages[0].Content, "gpt-5.6-luna") {
		t.Fatalf("policy prompt was not sent: %+v", classifier.req.Messages)
	}
}
