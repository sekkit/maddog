package skilleval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestScorePromotionUsesDeterministicFallbackWhenFrontierUnavailable(t *testing.T) {
	result, err := ScorePromotion(context.Background(), ReplayReport{
		CandidateID:       "cand-1",
		HeldOutCases:      2,
		BaselinePassRate:  0.25,
		CandidatePassRate: 0.75,
	}, ScoreOptions{
		MinHeldOut: 2,
		Frontier: FrontierScorerFunc(func(ctx context.Context, report ReplayReport) (ScoreResult, error) {
			return ScoreResult{}, errors.New("frontier unavailable")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionPromotable || !result.FrontierUnavailable || !strings.Contains(result.Reason, "pass-rate improvement") {
		t.Fatalf("fallback score = %+v", result)
	}
}

func TestScorePromotionKeepsTokenReductionReviewNeeded(t *testing.T) {
	result, err := ScorePromotion(context.Background(), ReplayReport{
		CandidateID:       "cand-1",
		HeldOutCases:      3,
		BaselinePassRate:  1,
		CandidatePassRate: 1,
		TokenDeltaPercent: -35,
	}, ScoreOptions{MinHeldOut: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionReviewNeeded || !strings.Contains(result.Reason, "token reduction") {
		t.Fatalf("token-only improvement should need review: %+v", result)
	}
}
