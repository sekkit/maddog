package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
)

const (
	officialAuthSmokeMode     = "official-auth-smoke"
	openAIOfficialTokenEnv    = "OPENAI_OFFICIAL_TOKEN"
	anthropicIdentityTokenEnv = "ANTHROPIC_IDENTITY_TOKEN"
	anthropicFederationEnv    = "ANTHROPIC_FEDERATION_RULE_ID"
	anthropicOrganizationEnv  = "ANTHROPIC_ORGANIZATION_ID"
	anthropicServiceAcctEnv   = "ANTHROPIC_SERVICE_ACCOUNT_ID"
	anthropicWorkspaceEnv     = "ANTHROPIC_WORKSPACE_ID"
)

type officialAuthSmokeConfig struct {
	OpenAIBaseURL    string `json:"openai_base_url"`
	OpenAIModel      string `json:"openai_model"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	AnthropicModel   string `json:"anthropic_model"`
	TimeoutSec       int    `json:"timeout_sec"`
}

type officialAuthSmokeResult struct {
	Passed            bool                    `json:"passed"`
	OpenAI            officialProviderSmoke   `json:"openai"`
	Anthropic         officialProviderSmoke   `json:"anthropic"`
	MissingCredential []string                `json:"missing_credentials,omitempty"`
	Note              string                  `json:"note,omitempty"`
	Config            officialAuthSmokeConfig `json:"config"`
}

type officialProviderSmoke struct {
	Passed bool   `json:"passed"`
	Model  string `json:"model"`
	Error  string `json:"error,omitempty"`
}

func runOfficialAuthSmoke(cfg officialAuthSmokeConfig) officialAuthSmokeResult {
	cfg = normalizeOfficialAuthSmokeConfig(cfg)
	result := officialAuthSmokeResult{
		Config:    cfg,
		OpenAI:    officialProviderSmoke{Model: cfg.OpenAIModel},
		Anthropic: officialProviderSmoke{Model: cfg.AnthropicModel},
	}
	for _, name := range []string{openAIOfficialTokenEnv, anthropicIdentityTokenEnv} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			result.MissingCredential = append(result.MissingCredential, name)
		}
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

	result.OpenAI = smokeOfficialOpenAI(ctx, cfg)
	result.Anthropic = smokeOfficialAnthropic(ctx, cfg)
	result.Passed = result.OpenAI.Passed && result.Anthropic.Passed
	result.Note = fmt.Sprintf("openai=%s; anthropic=%s", passStatus(result.OpenAI.Passed), passStatus(result.Anthropic.Passed))
	return result
}

func normalizeOfficialAuthSmokeConfig(cfg officialAuthSmokeConfig) officialAuthSmokeConfig {
	if strings.TrimSpace(cfg.OpenAIBaseURL) == "" {
		cfg.OpenAIBaseURL = "https://api.openai.com/v1"
	}
	if strings.TrimSpace(cfg.OpenAIModel) == "" {
		cfg.OpenAIModel = "gpt-4.1-mini"
	}
	if strings.TrimSpace(cfg.AnthropicBaseURL) == "" {
		cfg.AnthropicBaseURL = "https://api.anthropic.com"
	}
	if strings.TrimSpace(cfg.AnthropicModel) == "" {
		cfg.AnthropicModel = "claude-sonnet-4-6"
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 60
	}
	return cfg
}

func smokeOfficialOpenAI(ctx context.Context, cfg officialAuthSmokeConfig) officialProviderSmoke {
	smoke := officialProviderSmoke{Model: cfg.OpenAIModel}
	prov, err := provider.New("openai", provider.Config{
		Name:    "official-openai",
		BaseURL: cfg.OpenAIBaseURL,
		Model:   cfg.OpenAIModel,
		Extra: map[string]any{
			"proxy_spec":     netclient.ProxySpec{Mode: netclient.ModeEnv},
			"auth_type":      "bearer",
			"auth_token":     strings.TrimSpace(os.Getenv(openAIOfficialTokenEnv)),
			"auth_token_env": openAIOfficialTokenEnv,
			"auth_header":    "Authorization",
		},
	})
	if err != nil {
		smoke.Error = err.Error()
		return smoke
	}
	smoke.Passed, smoke.Error = drainProviderStream(ctx, prov, "Reply with the single word: ok.")
	return smoke
}

func smokeOfficialAnthropic(ctx context.Context, cfg officialAuthSmokeConfig) officialProviderSmoke {
	smoke := officialProviderSmoke{Model: cfg.AnthropicModel}
	extra := map[string]any{
		"proxy_spec":     netclient.ProxySpec{Mode: netclient.ModeEnv},
		"auth_type":      "workload_identity",
		"identity_token": strings.TrimSpace(os.Getenv(anthropicIdentityTokenEnv)),
		"identity_env":   anthropicIdentityTokenEnv,
	}
	for field, envName := range map[string]string{
		"federation_rule_id": anthropicFederationEnv,
		"organization_id":    anthropicOrganizationEnv,
		"service_account_id": anthropicServiceAcctEnv,
		"workspace_id":       anthropicWorkspaceEnv,
	} {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			extra[field] = value
		}
	}
	prov, err := provider.New("anthropic", provider.Config{
		Name:    "official-anthropic",
		BaseURL: cfg.AnthropicBaseURL,
		Model:   cfg.AnthropicModel,
		Extra:   extra,
	})
	if err != nil {
		smoke.Error = err.Error()
		return smoke
	}
	smoke.Passed, smoke.Error = drainProviderStream(ctx, prov, "Reply with the single word: ok.")
	return smoke
}

func drainProviderStream(ctx context.Context, prov provider.Provider, prompt string) (bool, string) {
	ch, err := prov.Stream(ctx, provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: prompt}},
		MaxTokens: 16,
	})
	if err != nil {
		return false, err.Error()
	}
	seen := false
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkError:
			if chunk.Err != nil {
				return false, chunk.Err.Error()
			}
			return false, "provider stream returned an error chunk"
		case provider.ChunkText, provider.ChunkReasoning, provider.ChunkUsage, provider.ChunkDone:
			seen = true
		}
	}
	if !seen {
		return false, "provider stream ended without any chunks"
	}
	return true, ""
}

func passStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "fail"
}

func renderOfficialAuthSmoke(r officialAuthSmokeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Maddog official auth smoke\n\n")
	fmt.Fprintf(&b, "**Result:** %s\n\n", passStatus(r.Passed))
	fmt.Fprintf(&b, "| Provider | Result | Model | Detail |\n")
	fmt.Fprintf(&b, "|----------|--------|-------|--------|\n")
	fmt.Fprintf(&b, "| official-openai | %s | `%s` | %s |\n",
		passStatus(r.OpenAI.Passed), r.OpenAI.Model, markdownCell(r.OpenAI.Error))
	fmt.Fprintf(&b, "| official-anthropic | %s | `%s` | %s |\n",
		passStatus(r.Anthropic.Passed), r.Anthropic.Model, markdownCell(r.Anthropic.Error))
	if len(r.MissingCredential) > 0 {
		fmt.Fprintf(&b, "\nMissing credentials: `%s`\n", strings.Join(r.MissingCredential, "`, `"))
	}
	if strings.TrimSpace(r.Note) != "" {
		fmt.Fprintf(&b, "\n%s\n", r.Note)
	}
	return b.String()
}

func markdownCell(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}
