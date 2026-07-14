package boot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"maddog/internal/agent"
	"maddog/internal/config"
	"maddog/internal/provider"
)

func TestResolveAdvisorRoutingPrefersExplicitModelWithoutUpgrade(t *testing.T) {
	advisorPrice := &provider.Pricing{Input: 5, Output: 25, Currency: "$"}
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "anthropic", Model: "claude-haiku-4-5"},
		config.ProviderEntry{Name: "advisor", Kind: "anthropic", Model: "claude-opus-4-8", Price: advisorPrice},
		config.ProviderEntry{Name: "frontier", Kind: "anthropic", Model: "claude-fable-5"},
	)
	cfg.Agent.AdvisorModel = "advisor"
	cfg.Agent.FrontierModel = "frontier"
	cfg.Agent.UpgradeEnabled = false
	cfg.Agent.AdvisorNativeEnabled = true
	cfg.Agent.AdvisorNativeCacheTTL = "5m"

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry == nil || route.NativeEntry.Model != "claude-opus-4-8" {
		t.Fatalf("native advisor = %+v, want explicit advisor_model", route.NativeEntry)
	}
	if route.NativeSource != advisorModelSourceExplicit {
		t.Fatalf("native source = %q, want advisor_model", route.NativeSource)
	}
	if route.NativeEntry.Price == nil || route.NativeEntry.Price.Input != advisorPrice.Input || route.NativeEntry.Price.Output != advisorPrice.Output || route.NativeEntry.Price.Currency != advisorPrice.Currency {
		t.Fatalf("advisor pricing = %+v, want configured per-model price", route.NativeEntry.Price)
	}
	if route.CacheTTL != "5m" {
		t.Fatalf("cache TTL = %q, want 5m", route.CacheTTL)
	}
	native, pricing := buildNativeAdvisorConfig(route, cfg.Agent)
	if native == nil || native.Model != "claude-opus-4-8" || native.MaxUses != 1 || native.MaxTokens != 2048 || native.CachingTTL != "5m" {
		t.Fatalf("native advisor config = %+v", native)
	}
	if pricing == nil || pricing.Input != 5 || pricing.Output != 25 || pricing.Currency != "$" {
		t.Fatalf("native advisor pricing = %+v", pricing)
	}
}

func TestResolveAdvisorRoutingWorksWithoutFrontierModel(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "anthropic", Model: "claude-sonnet-4-6"},
		config.ProviderEntry{Name: "advisor", Kind: "anthropic", Model: "claude-opus-4-8"},
	)
	cfg.Agent.AdvisorModel = "advisor"
	cfg.Agent.FrontierModel = ""
	cfg.Agent.AdvisorNativeEnabled = true

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry == nil || route.NativeEntry.Model != "claude-opus-4-8" {
		t.Fatalf("native advisor without frontier = %+v", route.NativeEntry)
	}
}

func TestResolveAdvisorRoutingFallsBackToFrontierSource(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "anthropic", Model: "claude-haiku-4-5"},
		config.ProviderEntry{Name: "frontier", Kind: "anthropic", Model: "claude-opus-4-8"},
	)
	cfg.Agent.AdvisorModel = ""
	cfg.Agent.FrontierModel = "frontier"
	cfg.Agent.UpgradeEnabled = false
	cfg.Agent.AdvisorNativeEnabled = true

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry == nil || route.NativeSource != advisorModelSourceFrontier {
		t.Fatalf("native route = %+v, source %q; want frontier compatibility source", route.NativeEntry, route.NativeSource)
	}
}

func TestResolveAdvisorRoutingRejectsKnownAnthropicPair(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "anthropic", Model: "claude-opus-4-8"},
		config.ProviderEntry{Name: "advisor", Kind: "anthropic", Model: "claude-sonnet-4-6"},
	)
	cfg.Agent.AdvisorModel = "advisor"
	cfg.Agent.AdvisorNativeEnabled = true

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry != nil {
		t.Fatalf("known incompatible pair produced native advisor: %+v", route.NativeEntry)
	}
	if route.FallbackAllowed {
		t.Fatal("known weaker explicit advisor should disable fallback")
	}
	assertAdvisorWarningContains(t, route.Warnings, "known incompatible Anthropic")
	assertAdvisorWarningContains(t, route.Warnings, "known weaker than executor")
}

func TestResolveAdvisorRoutingRejectsLunaAdvisor(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "openai", Model: "gpt-5.6-sol"},
		config.ProviderEntry{Name: "advisor", Kind: "openai", Model: "gpt-5.6-luna"},
	)
	cfg.Agent.AdvisorModel = "advisor"

	route := resolveAdvisorRouting(cfg, executor)
	if route.FallbackAllowed || route.NativeEntry != nil {
		t.Fatalf("Luna advisor route should be disabled: %+v", route)
	}
	assertAdvisorWarningContains(t, route.Warnings, "known low-capability model")
}

func TestValidateFallbackAdvisorModelRejectsImplicitLunaExecutor(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "openai", Model: "gpt-5.6-luna"},
	)
	allowed, warnings := validateFallbackAdvisorModel(cfg, executor, "")
	if allowed {
		t.Fatal("implicit Luna executor remained available as fallback advisor")
	}
	assertAdvisorWarningContains(t, warnings, "cannot serve as advisor")
}

func TestResolveAdvisorRoutingDisablesKnownWeakerFallback(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "openai", Model: "deepseek-v4-pro"},
		config.ProviderEntry{Name: "advisor", Kind: "openai", Model: "deepseek-v4-flash"},
	)
	cfg.Agent.AdvisorModel = "advisor"

	route := resolveAdvisorRouting(cfg, executor)
	if route.FallbackAllowed {
		t.Fatal("known weaker explicit fallback advisor remained enabled")
	}
	assertAdvisorWarningContains(t, route.Warnings, "fallback advisor disabled")
}

func TestResolveAdvisorRoutingAllowsUnknownFutureAnthropicPair(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "anthropic", Model: "claude-sonnet-6-custom"},
		config.ProviderEntry{Name: "advisor", Kind: "anthropic", Model: "claude-opus-6-custom"},
	)
	cfg.Agent.AdvisorModel = "advisor"
	cfg.Agent.AdvisorNativeEnabled = true

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry == nil {
		t.Fatalf("unknown future pair was hard-blocked: warnings=%v", route.Warnings)
	}
	assertAdvisorWarningContains(t, route.Warnings, "forward compatibility")
}

func TestResolveAdvisorRoutingRequiresAnthropicKindsForNative(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "openai", Model: "gpt-5.6-sol"},
		config.ProviderEntry{Name: "advisor", Kind: "anthropic", Model: "claude-opus-4-8"},
	)
	cfg.Agent.AdvisorModel = "advisor"
	cfg.Agent.AdvisorNativeEnabled = true

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry != nil {
		t.Fatalf("mixed provider kinds produced native advisor: %+v", route.NativeEntry)
	}
	if !route.FallbackAllowed {
		t.Fatal("mixed provider kinds should retain cross-provider fallback")
	}
	assertAdvisorWarningContains(t, route.Warnings, "requires anthropic executor and advisor kinds")
}

func TestResolveAdvisorRoutingInvalidCacheTTLDisablesOnlyCaching(t *testing.T) {
	cfg, executor := advisorRoutingConfig(
		config.ProviderEntry{Name: "executor", Kind: "anthropic", Model: "claude-haiku-4-5"},
		config.ProviderEntry{Name: "advisor", Kind: "anthropic", Model: "claude-opus-4-8"},
	)
	cfg.Agent.AdvisorModel = "advisor"
	cfg.Agent.AdvisorNativeEnabled = true
	cfg.Agent.AdvisorNativeCacheTTL = "30m"

	route := resolveAdvisorRouting(cfg, executor)
	if route.NativeEntry == nil {
		t.Fatalf("invalid cache TTL disabled native advisor: warnings=%v", route.Warnings)
	}
	if route.CacheTTL != "" {
		t.Fatalf("invalid cache TTL normalized to %q, want disabled", route.CacheTTL)
	}
	assertAdvisorWarningContains(t, route.Warnings, "advisor-side caching disabled")
}

func TestBuildInjectsAdvisorStrategyPromptOnlyWhenAvailable(t *testing.T) {
	tests := []struct {
		name    string
		maxUses int
		want    bool
	}{
		{name: "fallback available", maxUses: 1, want: true},
		{name: "advisor disabled", maxUses: 0, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "maddog.toml", `
default_model = "executor"

[agent]
system_prompt = "BASE"
advisor_model = "advisor"
advisor_max_uses_per_turn = `+fmt.Sprint(tt.maxUses)+`
advisor_native_enabled = false

[[providers]]
name = "executor"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "deepseek-v4-flash"
api_key_env = "MADDOG_ADVISOR_EXECUTOR_KEY_UNSET"

[[providers]]
name = "advisor"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "deepseek-v4-pro"
api_key_env = "MADDOG_ADVISOR_MODEL_KEY_UNSET"
`)

			ctrl, err := Build(context.Background(), Options{})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			got := strings.Contains(systemMessage(ctrl.History()), agent.AdvisorStrategyPrompt)
			if got != tt.want {
				t.Fatalf("strategy prompt present = %v, want %v\nsystem:\n%s", got, tt.want, systemMessage(ctrl.History()))
			}
		})
	}
}

func advisorRoutingConfig(entries ...config.ProviderEntry) (*config.Config, *config.ProviderEntry) {
	cfg := config.Default()
	cfg.Providers = entries
	cfg.Agent.AdvisorMaxUsesPerTurn = 1
	executor, ok := cfg.ResolveModel(entries[0].Name)
	if !ok {
		panic("test executor did not resolve")
	}
	return cfg, executor
}

func assertAdvisorWarningContains(t *testing.T, warnings []string, want string) {
	t.Helper()
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return
		}
	}
	t.Fatalf("warnings %v do not contain %q", warnings, want)
}
