package boot

import (
	"fmt"
	"strings"

	"maddog/internal/config"
	"maddog/internal/provider"
)

type advisorModelSource string

const (
	advisorModelSourceNone     advisorModelSource = ""
	advisorModelSourceExplicit advisorModelSource = "advisor_model"
	advisorModelSourceFrontier advisorModelSource = "frontier_model"
)

type advisorRouting struct {
	NativeEntry     *config.ProviderEntry
	NativeSource    advisorModelSource
	FallbackAllowed bool
	CacheTTL        string
	Warnings        []string
}

type modelCapability struct {
	family   string
	tier     int
	nativeID string
}

type nativePairCompatibility int

const (
	nativePairUnknown nativePairCompatibility = iota
	nativePairCompatible
	nativePairIncompatible
)

// This mirrors Anthropic's advisor beta compatibility table. Unknown IDs are
// deliberately handled outside the table so future models warn instead of
// being rejected.
var nativeAnthropicAdvisorPairs = map[string]map[string]bool{
	"claude-haiku-4-5": {
		"claude-fable-5": true, "claude-mythos-5": true,
		"claude-opus-4-8": true, "claude-opus-4-7": true,
		"claude-opus-4-6": true, "claude-sonnet-4-6": true,
	},
	"claude-sonnet-4-6": {
		"claude-fable-5": true, "claude-mythos-5": true,
		"claude-opus-4-8": true, "claude-opus-4-7": true,
		"claude-opus-4-6": true, "claude-sonnet-4-6": true,
	},
	"claude-sonnet-5": {
		"claude-fable-5": true, "claude-mythos-5": true,
		"claude-opus-4-8": true, "claude-opus-4-7": true,
	},
	"claude-opus-4-6": {
		"claude-fable-5": true, "claude-mythos-5": true,
		"claude-opus-4-8": true, "claude-opus-4-7": true,
		"claude-opus-4-6": true,
	},
	"claude-opus-4-7": {
		"claude-fable-5": true, "claude-mythos-5": true,
		"claude-opus-4-8": true, "claude-opus-4-7": true,
	},
	"claude-opus-4-8": {
		"claude-fable-5": true, "claude-mythos-5": true,
		"claude-opus-4-8": true, "claude-opus-4-7": true,
	},
	"claude-fable-5":  {"claude-fable-5": true},
	"claude-mythos-5": {"claude-mythos-5": true},
}

func resolveAdvisorRouting(cfg *config.Config, executor *config.ProviderEntry) advisorRouting {
	route := advisorRouting{FallbackAllowed: cfg != nil && cfg.Agent.AdvisorMaxUsesPerTurn > 0}
	if cfg == nil || executor == nil || !route.FallbackAllowed {
		return route
	}

	if ttl, ok := config.NormalizeAdvisorNativeCacheTTL(cfg.Agent.AdvisorNativeCacheTTL); ok {
		route.CacheTTL = ttl
	} else if cfg.Agent.AdvisorNativeEnabled {
		route.warn(fmt.Sprintf("advisor_native_cache_ttl %q is invalid; advisor-side caching disabled (use empty, 5m, or 1h)", cfg.Agent.AdvisorNativeCacheTTL))
	}

	explicitRef := strings.TrimSpace(cfg.Agent.AdvisorModel)
	var nativeRef string
	if explicitRef != "" {
		nativeRef = explicitRef
		route.NativeSource = advisorModelSourceExplicit
		explicit, ok := cfg.ResolveModel(explicitRef)
		if !ok {
			route.FallbackAllowed = false
			route.warn(fmt.Sprintf("advisor_model %q is not configured; advisor disabled", explicitRef))
			return route
		}
		if advisorModelProhibited(explicit.Model) {
			route.FallbackAllowed = false
			route.warn(fmt.Sprintf("advisor_model %q resolves to %q, a known low-capability model that cannot serve as advisor; advisor disabled", explicitRef, explicit.Model))
			return route
		}
		if weaker, comparable := knownAdvisorWeaker(executor.Model, explicit.Model); comparable {
			if weaker {
				route.FallbackAllowed = false
				route.warn(fmt.Sprintf("advisor_model %q (%s) is known weaker than executor %q; fallback advisor disabled", explicitRef, explicit.Model, executor.Model))
			}
		} else {
			route.warn(fmt.Sprintf("advisor capability comparison for executor %q and advisor %q is unknown; fallback remains enabled for forward compatibility", executor.Model, explicit.Model))
		}
	} else if frontierRef := strings.TrimSpace(cfg.Agent.FrontierModel); frontierRef != "" {
		nativeRef = frontierRef
		route.NativeSource = advisorModelSourceFrontier
	}

	if !cfg.Agent.AdvisorNativeEnabled {
		return route
	}
	if nativeRef == "" {
		route.warn("advisor_native_enabled is true but neither advisor_model nor frontier_model is configured; native advisor disabled")
		return route
	}

	advisorEntry, ok := cfg.ResolveModel(nativeRef)
	if !ok {
		route.warn(fmt.Sprintf("%s %q is not configured; native advisor disabled", route.NativeSource, nativeRef))
		return route
	}
	if advisorModelProhibited(advisorEntry.Model) {
		route.warn(fmt.Sprintf("%s %q resolves to %q, a known low-capability model that cannot serve as advisor; native advisor disabled", route.NativeSource, nativeRef, advisorEntry.Model))
		return route
	}
	if !isAnthropicKind(executor.Kind) || !isAnthropicKind(advisorEntry.Kind) {
		route.warn(fmt.Sprintf("native advisor requires anthropic executor and advisor kinds (got %q and %q); native advisor disabled", executor.Kind, advisorEntry.Kind))
		return route
	}

	switch knownNativePair(executor.Model, advisorEntry.Model) {
	case nativePairIncompatible:
		route.warn(fmt.Sprintf("executor %q and advisor %q are a known incompatible Anthropic native advisor pair; native advisor disabled", executor.Model, advisorEntry.Model))
		return route
	case nativePairUnknown:
		route.warn(fmt.Sprintf("native advisor pair executor=%q advisor=%q is not in the known capability table; allowing it for forward compatibility", executor.Model, advisorEntry.Model))
	}

	route.NativeEntry = advisorEntry
	return route
}

func buildNativeAdvisorConfig(route advisorRouting, cfg config.AgentConfig) (*provider.NativeAdvisorConfig, *provider.Pricing) {
	if route.NativeEntry == nil {
		return nil, nil
	}
	return &provider.NativeAdvisorConfig{
		Model:      route.NativeEntry.Model,
		MaxUses:    cfg.AdvisorMaxUsesPerTurn,
		MaxTokens:  cfg.AdvisorNativeMaxTokens,
		CachingTTL: route.CacheTTL,
	}, route.NativeEntry.Price
}

func validateFallbackAdvisorModel(cfg *config.Config, executor *config.ProviderEntry, modelRef string) (bool, []string) {
	if cfg == nil || executor == nil {
		return false, nil
	}
	entry := executor
	modelRef = strings.TrimSpace(modelRef)
	if modelRef != "" {
		resolved, ok := cfg.ResolveModel(modelRef)
		if !ok {
			return false, []string{fmt.Sprintf("fallback advisor model %q is not configured; fallback advisor disabled", modelRef)}
		}
		entry = resolved
	}
	if advisorModelProhibited(entry.Model) {
		return false, []string{fmt.Sprintf("fallback advisor resolves to %q, a known low-capability model that cannot serve as advisor; fallback advisor disabled", entry.Model)}
	}
	if strings.TrimSpace(cfg.Agent.AdvisorModel) == "" {
		if _, known := knownModelCapability(entry.Model); !known {
			return true, []string{fmt.Sprintf("fallback advisor capability for model %q is unknown; allowing it for forward compatibility", entry.Model)}
		}
	}
	return true, nil
}

func (r *advisorRouting) warn(message string) {
	for _, existing := range r.Warnings {
		if existing == message {
			return
		}
	}
	r.Warnings = append(r.Warnings, message)
}

func isAnthropicKind(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), "anthropic")
}

func advisorModelProhibited(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "gpt-5.6-luna")
}

func knownAdvisorWeaker(executorModel, advisorModel string) (weaker, comparable bool) {
	executor, executorKnown := knownModelCapability(executorModel)
	advisor, advisorKnown := knownModelCapability(advisorModel)
	if !executorKnown || !advisorKnown || executor.family != advisor.family {
		return false, false
	}
	return advisor.tier < executor.tier, true
}

func knownNativePair(executorModel, advisorModel string) nativePairCompatibility {
	executor, executorKnown := knownModelCapability(executorModel)
	advisor, advisorKnown := knownModelCapability(advisorModel)
	if !executorKnown || !advisorKnown {
		return nativePairUnknown
	}
	if executor.family != "anthropic" || advisor.family != "anthropic" || executor.nativeID == "" || advisor.nativeID == "" {
		return nativePairIncompatible
	}
	if nativeAnthropicAdvisorPairs[executor.nativeID][advisor.nativeID] {
		return nativePairCompatible
	}
	return nativePairIncompatible
}

func knownModelCapability(model string) (modelCapability, bool) {
	// Tiers are comparable only inside one family; cross-family comparisons stay
	// unknown and therefore advisory rather than blocking.
	id := strings.ToLower(strings.TrimSpace(model))
	for _, known := range []modelCapability{
		{family: "anthropic", tier: 50, nativeID: "claude-fable-5"},
		{family: "anthropic", tier: 50, nativeID: "claude-mythos-5"},
		{family: "anthropic", tier: 40, nativeID: "claude-opus-4-8"},
		{family: "anthropic", tier: 40, nativeID: "claude-opus-4-7"},
		{family: "anthropic", tier: 35, nativeID: "claude-sonnet-5"},
		{family: "anthropic", tier: 30, nativeID: "claude-opus-4-6"},
		{family: "anthropic", tier: 20, nativeID: "claude-sonnet-4-6"},
		{family: "anthropic", tier: 10, nativeID: "claude-haiku-4-5"},
	} {
		if modelIDMatches(id, known.nativeID) {
			return known, true
		}
	}

	for _, old := range []struct {
		id   string
		tier int
	}{
		{id: "claude-opus-4-5", tier: 28},
		{id: "claude-opus-4-1", tier: 26},
		{id: "claude-opus-4", tier: 25},
		{id: "claude-sonnet-4-5", tier: 18},
		{id: "claude-sonnet-4", tier: 16},
		{id: "claude-3-7-sonnet", tier: 15},
		{id: "claude-3-5-sonnet", tier: 14},
		{id: "claude-3-5-haiku", tier: 8},
		{id: "claude-3-haiku", tier: 6},
		{id: "claude-3-opus", tier: 20},
	} {
		if modelIDMatches(id, old.id) {
			return modelCapability{family: "anthropic", tier: old.tier}, true
		}
	}

	switch id {
	case "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra":
		return modelCapability{family: "gpt-5.6", tier: 40}, true
	case "gpt-5.6-luna":
		return modelCapability{family: "gpt-5.6", tier: 10}, true
	case "deepseek-v4-pro":
		return modelCapability{family: "deepseek-v4", tier: 20}, true
	case "deepseek-v4-flash":
		return modelCapability{family: "deepseek-v4", tier: 10}, true
	default:
		return modelCapability{}, false
	}
}

func modelIDMatches(model, canonical string) bool {
	return model == canonical || strings.HasPrefix(model, canonical+"-20")
}
