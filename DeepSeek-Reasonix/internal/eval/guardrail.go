package eval

import "fmt"

type GuardrailResult struct {
	Pass   bool
	Reason string
}

type GuardrailConfig struct {
	MinBundles int
	MinScore   float64
}

func CheckGuardrail(bundles []ReplayBundle, oldResults, newResults []OutcomeInfo, scores []float64, cfg GuardrailConfig) GuardrailResult {
	if cfg.MinBundles <= 0 {
		cfg.MinBundles = 5
	}
	if cfg.MinScore <= 0 {
		cfg.MinScore = 0.7
	}
	if len(bundles) < cfg.MinBundles {
		return GuardrailResult{Reason: fmt.Sprintf("need at least %d bundles, got %d", cfg.MinBundles, len(bundles))}
	}
	if len(newResults) < len(bundles) || len(oldResults) < len(bundles) || len(scores) < len(bundles) {
		return GuardrailResult{Reason: "missing replay or score results"}
	}
	oldRate := successRate(oldResults[:len(bundles)])
	newRate := successRate(newResults[:len(bundles)])
	if newRate < oldRate {
		return GuardrailResult{Reason: fmt.Sprintf("regression: new success %.2f < old %.2f", newRate, oldRate)}
	}
	for i := 0; i < len(bundles); i++ {
		if scores[i] < cfg.MinScore {
			return GuardrailResult{Reason: fmt.Sprintf("score %.2f below threshold %.2f", scores[i], cfg.MinScore)}
		}
	}
	return GuardrailResult{Pass: true, Reason: fmt.Sprintf("passed guardrail: success %.2f >= %.2f", newRate, oldRate)}
}

func successRate(results []OutcomeInfo) float64 {
	if len(results) == 0 {
		return 0
	}
	ok := 0
	for _, r := range results {
		if r.Success && r.GoalMet {
			ok++
		}
	}
	return float64(ok) / float64(len(results))
}
