package main

import (
	"context"
	"testing"

	"maddog/internal/config"
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
