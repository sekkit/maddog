package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"maddog/internal/eval"
	"maddog/internal/netclient"
	"maddog/internal/provider"
	_ "maddog/internal/provider/anthropic"
	"maddog/internal/provider/costwrap"
	_ "maddog/internal/provider/openai"
)

const (
	frontierSmokeMode    = "frontier-smoke"
	icodeEasyAPIKeyEnv   = "ICODEEASY_API_KEY"
	openAIAPIKeyEnv      = "OPENAI_API_KEY"
	anthropicAPIKeyEnv   = "ANTHROPIC_API_KEY"
	defaultIcodeEasyURL  = "https://api.icodeeasy.cc/v1"
	defaultOpenAIURL     = "https://api.openai.com/v1"
	defaultAnthropicURL  = "https://api.anthropic.com"
	defaultFrontierModel = "gpt-5.5"
)

type frontierSmokeConfig struct {
	OpenAIBaseURL    string `json:"openai_base_url"`
	OpenAIModel      string `json:"openai_model"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	AnthropicModel   string `json:"anthropic_model"`
	TimeoutSec       int    `json:"timeout_sec"`
}

type frontierSmokeResult struct {
	Passed            bool                  `json:"passed"`
	Provider          officialProviderSmoke `json:"provider"`
	Costwrap          costwrapSmokeResult   `json:"costwrap"`
	Scorer            scorerSmokeResult     `json:"scorer"`
	AnthropicAdvisor  officialProviderSmoke `json:"anthropic_advisor"`
	MissingCredential []string              `json:"missing_credentials,omitempty"`
	Note              string                `json:"note,omitempty"`
	Config            frontierSmokeConfig   `json:"config"`
}

type costwrapSmokeResult struct {
	Passed       bool   `json:"passed"`
	OutputTokens int64  `json:"output_tokens"`
	Error        string `json:"error,omitempty"`
}

type scorerSmokeResult struct {
	Passed bool    `json:"passed"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
	Error  string  `json:"error,omitempty"`
}

func runFrontierSmoke(cfg frontierSmokeConfig) frontierSmokeResult {
	cfg = normalizeFrontierSmokeConfig(cfg)
	result := frontierSmokeResult{
		Config:           cfg,
		Provider:         officialProviderSmoke{Model: cfg.OpenAIModel},
		AnthropicAdvisor: officialProviderSmoke{Model: cfg.AnthropicModel},
	}
	apiKey, apiKeyEnv := firstSetEnv(icodeEasyAPIKeyEnv, openAIAPIKeyEnv)
	if apiKey == "" {
		result.MissingCredential = append(result.MissingCredential, icodeEasyAPIKeyEnv+" or "+openAIAPIKeyEnv)
	}
	if strings.TrimSpace(os.Getenv(anthropicAPIKeyEnv)) == "" {
		result.MissingCredential = append(result.MissingCredential, anthropicAPIKeyEnv)
	}
	if len(result.MissingCredential) > 0 {
		result.Note = "missing credentials: " + strings.Join(result.MissingCredential, ", ")
		return result
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	baseURL := cfg.OpenAIBaseURL
	if apiKeyEnv == openAIAPIKeyEnv && baseURL == defaultIcodeEasyURL {
		baseURL = defaultOpenAIURL
	}
	prov, err := provider.New("openai", provider.Config{
		Name:    "frontier-smoke",
		BaseURL: baseURL,
		Model:   cfg.OpenAIModel,
		APIKey:  apiKey,
		Extra: map[string]any{
			"proxy_spec":  netclient.ProxySpec{Mode: netclient.ModeEnv},
			"api_key_env": apiKeyEnv,
			"wire_api":    "responses",
			"effort":      "high",
		},
	})
	if err != nil {
		result.Provider.Error = err.Error()
		result.Note = summarizeFrontierSmoke(result)
		return result
	}

	var tokens atomic.Int64
	tracked := costwrap.New(prov, &tokens, 500000)
	result.Provider.Passed, result.Provider.Error = drainProviderStream(ctx, tracked, "Reply with the single word: ok.")
	result.Costwrap.OutputTokens = tokens.Load()
	result.Costwrap.Passed = result.Costwrap.OutputTokens > 0
	if !result.Costwrap.Passed {
		result.Costwrap.Error = "frontier provider did not report output tokens"
	}

	score, scoreErr := eval.Score(ctx, prov,
		eval.OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "ok", ToolErrors: 1},
		eval.OutcomeInfo{Success: true, GoalMet: true, FinalAnswer: "ok", ToolErrors: 0},
	)
	result.Scorer.Score = score.Score
	result.Scorer.Reason = score.Reason
	result.Scorer.Passed = scoreErr == nil && score.Score > 0
	if scoreErr != nil {
		result.Scorer.Error = scoreErr.Error()
	}

	result.AnthropicAdvisor = smokeFrontierAdvisor(ctx, cfg)
	result.Passed = result.Provider.Passed && result.Costwrap.Passed && result.Scorer.Passed && result.AnthropicAdvisor.Passed
	result.Note = summarizeFrontierSmoke(result)
	return result
}

func normalizeFrontierSmokeConfig(cfg frontierSmokeConfig) frontierSmokeConfig {
	if strings.TrimSpace(cfg.OpenAIBaseURL) == "" {
		cfg.OpenAIBaseURL = defaultIcodeEasyURL
	}
	if strings.TrimSpace(cfg.OpenAIModel) == "" {
		cfg.OpenAIModel = defaultFrontierModel
	}
	if strings.TrimSpace(cfg.AnthropicBaseURL) == "" {
		cfg.AnthropicBaseURL = defaultAnthropicURL
	}
	if strings.TrimSpace(cfg.AnthropicModel) == "" {
		cfg.AnthropicModel = "claude-sonnet-4-6"
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 60
	}
	return cfg
}

func smokeFrontierAdvisor(ctx context.Context, cfg frontierSmokeConfig) officialProviderSmoke {
	smoke := officialProviderSmoke{Model: cfg.AnthropicModel}
	prov, err := provider.New("anthropic", provider.Config{
		Name:    "frontier-advisor",
		BaseURL: cfg.AnthropicBaseURL,
		Model:   cfg.AnthropicModel,
		APIKey:  strings.TrimSpace(os.Getenv(anthropicAPIKeyEnv)),
		Extra: map[string]any{
			"proxy_spec":  netclient.ProxySpec{Mode: netclient.ModeEnv},
			"api_key_env": anthropicAPIKeyEnv,
		},
	})
	if err != nil {
		smoke.Error = err.Error()
		return smoke
	}
	ch, err := prov.Stream(ctx, provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "Reply with the single word: ok."}},
		MaxTokens: 16,
		NativeAdvisor: &provider.NativeAdvisorConfig{
			Model:     cfg.AnthropicModel,
			MaxUses:   1,
			MaxTokens: 256,
		},
	})
	if err != nil {
		smoke.Error = err.Error()
		return smoke
	}
	seen := false
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkError:
			if chunk.Err != nil {
				smoke.Error = chunk.Err.Error()
			} else {
				smoke.Error = "provider stream returned an error chunk"
			}
			return smoke
		case provider.ChunkText, provider.ChunkReasoning, provider.ChunkUsage, provider.ChunkDone:
			seen = true
		}
	}
	smoke.Passed = seen
	if !smoke.Passed {
		smoke.Error = "provider stream ended without any chunks"
	}
	return smoke
}

func firstSetEnv(names ...string) (string, string) {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, name
		}
	}
	return "", ""
}

func summarizeFrontierSmoke(r frontierSmokeResult) string {
	return fmt.Sprintf("provider=%s; costwrap=%s; scorer=%s; anthropic_advisor=%s",
		passStatus(r.Provider.Passed),
		passStatus(r.Costwrap.Passed),
		passStatus(r.Scorer.Passed),
		passStatus(r.AnthropicAdvisor.Passed),
	)
}

func renderFrontierSmoke(r frontierSmokeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Maddog frontier smoke\n\n")
	fmt.Fprintf(&b, "**Result:** %s\n\n", passStatus(r.Passed))
	fmt.Fprintf(&b, "| Gate | Result | Detail |\n")
	fmt.Fprintf(&b, "|------|--------|--------|\n")
	fmt.Fprintf(&b, "| provider | %s | %s |\n", passStatus(r.Provider.Passed), markdownCell(r.Provider.Error))
	fmt.Fprintf(&b, "| costwrap | %s | output_tokens=%d %s |\n", passStatus(r.Costwrap.Passed), r.Costwrap.OutputTokens, markdownCell(r.Costwrap.Error))
	fmt.Fprintf(&b, "| scorer | %s | score=%.2f %s |\n", passStatus(r.Scorer.Passed), r.Scorer.Score, markdownCell(firstNonEmpty(r.Scorer.Error, r.Scorer.Reason)))
	fmt.Fprintf(&b, "| anthropic_advisor | %s | %s |\n", passStatus(r.AnthropicAdvisor.Passed), markdownCell(r.AnthropicAdvisor.Error))
	if len(r.MissingCredential) > 0 {
		fmt.Fprintf(&b, "\nMissing credentials: `%s`\n", strings.Join(r.MissingCredential, "`, `"))
	}
	if strings.TrimSpace(r.Note) != "" {
		fmt.Fprintf(&b, "\n%s\n", r.Note)
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
