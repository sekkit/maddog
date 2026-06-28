package agent

import (
	"math"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type providerStatusTotals struct {
	RequestCount     int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheHitTokens   int
	CacheMissTokens  int
	ReasoningTokens  int
	Cost             float64
	Currency         string
}

func (a *Agent) emitProviderUsageStatus(u *provider.Usage) {
	if a == nil || u == nil {
		return
	}
	role := a.activeProviderRole()
	if role == "frontier" && u.CompletionTokens > 0 {
		a.frontierTokens.Add(int64(u.CompletionTokens))
	}
	if a.providerUsage == nil {
		a.providerUsage = map[string]*providerStatusTotals{}
	}
	totals := a.providerUsage[role]
	if totals == nil {
		totals = &providerStatusTotals{}
		a.providerUsage[role] = totals
	}
	totals.RequestCount++
	totals.PromptTokens += positiveInt(u.PromptTokens)
	totals.CompletionTokens += positiveInt(u.CompletionTokens)
	totals.TotalTokens += positiveInt(u.TotalTokens)
	totals.CacheHitTokens += positiveInt(u.CacheHitTokens)
	totals.CacheMissTokens += positiveInt(u.CacheMissTokens)
	totals.ReasoningTokens += positiveInt(u.ReasoningTokens)
	if a.pricing != nil {
		totals.Cost += a.pricing.Cost(u)
		totals.Currency = a.pricing.Symbol()
	}
	status := a.providerStatusSnapshot(role, "active")
	status.RequestCount = totals.RequestCount
	status.PromptTokens = totals.PromptTokens
	status.CompletionTokens = totals.CompletionTokens
	status.TotalTokens = totals.TotalTokens
	status.CacheHitTokens = totals.CacheHitTokens
	status.CacheMissTokens = totals.CacheMissTokens
	status.ReasoningTokens = totals.ReasoningTokens
	status.Cost = roundCost(totals.Cost)
	status.Currency = totals.Currency
	a.sink.Emit(event.Event{Kind: event.ProviderStatus, Level: event.LevelInfo, ProviderStatus: status})
}

func (a *Agent) emitProviderRouteStatus(role, status string) {
	if a == nil {
		return
	}
	snapshot := a.providerStatusSnapshot(role, status)
	if totals := a.providerUsage[role]; totals != nil {
		snapshot.RequestCount = totals.RequestCount
		snapshot.PromptTokens = totals.PromptTokens
		snapshot.CompletionTokens = totals.CompletionTokens
		snapshot.TotalTokens = totals.TotalTokens
		snapshot.CacheHitTokens = totals.CacheHitTokens
		snapshot.CacheMissTokens = totals.CacheMissTokens
		snapshot.ReasoningTokens = totals.ReasoningTokens
		snapshot.Cost = roundCost(totals.Cost)
		snapshot.Currency = totals.Currency
	}
	level := event.LevelInfo
	if status == "budget_exceeded" || status == "degraded" {
		level = event.LevelWarn
	}
	a.sink.Emit(event.Event{Kind: event.ProviderStatus, Level: level, ProviderStatus: snapshot})
}

func (a *Agent) emitAdvisorProviderStatus() {
	if a == nil {
		return
	}
	snapshot := event.ProviderStatusSnapshot{
		Role:         "advisor",
		Provider:     "advisor",
		Model:        "advisor",
		Status:       "active",
		RequestCount: a.advisorSessionUses,
	}
	if a.nativeAdvisor != nil {
		if a.prov != nil {
			snapshot.Provider = a.prov.Name()
		}
		if strings.TrimSpace(a.nativeAdvisor.Model) != "" {
			snapshot.Model = strings.TrimSpace(a.nativeAdvisor.Model)
		}
	}
	a.sink.Emit(event.Event{Kind: event.ProviderStatus, Level: event.LevelInfo, ProviderStatus: snapshot})
}

func (a *Agent) activeProviderRole() string {
	if a != nil && a.onFrontier {
		return "frontier"
	}
	if a != nil && strings.TrimSpace(a.providerRole) != "" {
		return strings.TrimSpace(a.providerRole)
	}
	return "default"
}

func (a *Agent) providerStatusSnapshot(role, status string) event.ProviderStatusSnapshot {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "default"
	}
	out := event.ProviderStatusSnapshot{
		Role:          role,
		Status:        strings.TrimSpace(status),
		UpgradeReason: strings.TrimSpace(a.lastUpgradeReason),
	}
	switch role {
	case "frontier":
		if a.frontierProv != nil {
			out.Provider = a.frontierProv.Name()
		} else if a.prov != nil {
			out.Provider = a.prov.Name()
		}
		out.Model = strings.TrimSpace(a.frontierTarget)
	default:
		if a.defaultProv != nil {
			out.Provider = a.defaultProv.Name()
		} else if a.prov != nil {
			out.Provider = a.prov.Name()
		}
	}
	if out.Model == "" {
		out.Model = out.Provider
	}
	if role == "frontier" {
		out.BudgetUsedTokens, out.BudgetLimitTokens, out.BudgetRemainingTokens = a.frontierBudgetSnapshot()
	}
	return out
}

func (a *Agent) frontierBudgetSnapshot() (used, limit, remaining int64) {
	if a == nil {
		return 0, 0, 0
	}
	used = a.frontierTokens.Load()
	if tracked, ok := a.frontierProv.(interface {
		OutputTokens() int64
		BudgetLimit() int64
	}); ok {
		used = tracked.OutputTokens()
		limit = tracked.BudgetLimit()
		a.frontierTokens.Store(used)
	} else if limited, ok := a.upgradePolicy.(interface{ FrontierBudgetLimit() int64 }); ok {
		limit = limited.FrontierBudgetLimit()
	}
	if limit > 0 {
		remaining = limit - used
		if remaining < 0 {
			remaining = 0
		}
	}
	return used, limit, remaining
}

func positiveInt(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func roundCost(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*1e12) / 1e12
}
