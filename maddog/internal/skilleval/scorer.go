package skilleval

import (
	"context"
	"fmt"
	"strings"
)

type Decision string

const (
	DecisionPromotable   Decision = "promotable"
	DecisionReviewNeeded Decision = "review_needed"
	DecisionRejected     Decision = "rejected"
)

type ScoreOptions struct {
	MinHeldOut int
	Frontier   FrontierScorer
}

type FrontierScorer interface {
	ScoreCandidate(context.Context, ReplayReport) (ScoreResult, error)
}

type FrontierScorerFunc func(context.Context, ReplayReport) (ScoreResult, error)

func (f FrontierScorerFunc) ScoreCandidate(ctx context.Context, report ReplayReport) (ScoreResult, error) {
	return f(ctx, report)
}

type ScoreResult struct {
	CandidateID         string   `json:"candidateId"`
	Decision            Decision `json:"decision"`
	Score               float64  `json:"score,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	FrontierUnavailable bool     `json:"frontierUnavailable,omitempty"`
}

func ScorePromotion(ctx context.Context, report ReplayReport, opts ScoreOptions) (ScoreResult, error) {
	if opts.Frontier != nil {
		result, err := opts.Frontier.ScoreCandidate(ctx, report)
		if err == nil {
			if result.CandidateID == "" {
				result.CandidateID = report.CandidateID
			}
			return result, nil
		}
		fallback := deterministicScore(report, opts)
		fallback.FrontierUnavailable = true
		if fallback.Reason == "" {
			fallback.Reason = "frontier scorer unavailable; used deterministic fallback"
		} else if !strings.Contains(fallback.Reason, "frontier") {
			fallback.Reason += " (frontier scorer unavailable; used deterministic fallback)"
		}
		return fallback, nil
	}
	return deterministicScore(report, opts), nil
}

func deterministicScore(report ReplayReport, opts ScoreOptions) ScoreResult {
	minHeldOut := opts.MinHeldOut
	if minHeldOut <= 0 {
		minHeldOut = report.MinHeldOut
	}
	if minHeldOut > 0 && report.HeldOutCases < minHeldOut || report.InsufficientHeldOut {
		return ScoreResult{
			CandidateID: report.CandidateID,
			Decision:    DecisionReviewNeeded,
			Score:       0.2,
			Reason:      fmt.Sprintf("insufficient held-out bundles: %d/%d", report.HeldOutCases, minHeldOut),
		}
	}
	passDelta := report.CandidatePassRate - report.BaselinePassRate
	switch {
	case passDelta > 0:
		return ScoreResult{
			CandidateID: report.CandidateID,
			Decision:    DecisionPromotable,
			Score:       clamp(0.7 + passDelta*0.3),
			Reason:      fmt.Sprintf("pass-rate improvement %.0f%% -> %.0f%%", report.BaselinePassRate*100, report.CandidatePassRate*100),
		}
	case passDelta < 0:
		return ScoreResult{
			CandidateID: report.CandidateID,
			Decision:    DecisionRejected,
			Score:       0.1,
			Reason:      fmt.Sprintf("pass-rate regression %.0f%% -> %.0f%%", report.BaselinePassRate*100, report.CandidatePassRate*100),
		}
	case report.TokenDeltaPercent < 0:
		return ScoreResult{
			CandidateID: report.CandidateID,
			Decision:    DecisionReviewNeeded,
			Score:       0.55,
			Reason:      fmt.Sprintf("token reduction %.1f%% without pass-rate improvement", -report.TokenDeltaPercent),
		}
	default:
		return ScoreResult{
			CandidateID: report.CandidateID,
			Decision:    DecisionReviewNeeded,
			Score:       0.5,
			Reason:      "no deterministic improvement",
		}
	}
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
