package skilleval

import (
	"context"
	"fmt"
)

type ReplayOptions struct {
	Candidate              Candidate
	Bundles                []Bundle
	MinHeldOut             int
	IncludeHarnessProposal bool
	Evaluator              ReplayEvaluator
}

type ReplayEvaluator interface {
	EvaluateReplay(context.Context, Candidate, Bundle) (ReplayCaseReport, error)
}

type ReplayEvaluatorFunc func(context.Context, Candidate, Bundle) (ReplayCaseReport, error)

func (f ReplayEvaluatorFunc) EvaluateReplay(ctx context.Context, candidate Candidate, bundle Bundle) (ReplayCaseReport, error) {
	return f(ctx, candidate, bundle)
}

type ReplayReport struct {
	CandidateID            string             `json:"candidateId"`
	Cases                  []ReplayCaseReport `json:"cases"`
	ReplayCases            int                `json:"replayCases"`
	HeldOutCases           int                `json:"heldOutCases"`
	MinHeldOut             int                `json:"minHeldOut,omitempty"`
	InsufficientHeldOut    bool               `json:"insufficientHeldOut,omitempty"`
	BaselinePassRate       float64            `json:"baselinePassRate"`
	CandidatePassRate      float64            `json:"candidatePassRate"`
	BaselineTokens         int                `json:"baselineTokens,omitempty"`
	CandidateTokens        int                `json:"candidateTokens,omitempty"`
	TokenDeltaPercent      float64            `json:"tokenDeltaPercent,omitempty"`
	FailureCount           int                `json:"failureCount,omitempty"`
	LowConfidenceSkipCount int                `json:"lowConfidenceSkipCount,omitempty"`
	HarnessProposal        *HarnessProposal   `json:"harnessProposal,omitempty"`
}

type ReplayCaseReport struct {
	ID               string `json:"id"`
	BundleID         string `json:"bundleId"`
	CandidateSkillID string `json:"candidateSkillId,omitempty"`
	BaselinePassed   bool   `json:"baselinePassed"`
	CandidatePassed  bool   `json:"candidatePassed"`
	BaselineTokens   int    `json:"baselineTokens,omitempty"`
	CandidateTokens  int    `json:"candidateTokens,omitempty"`
	LowConfidence    bool   `json:"lowConfidence,omitempty"`
	Error            string `json:"error,omitempty"`
}

type HarnessProposal struct {
	ProposalID                string   `json:"proposalId"`
	CandidateDocs             int      `json:"candidateDocs,omitempty"`
	CuratedEvidence           int      `json:"curatedEvidence,omitempty"`
	VerificationRecords       int      `json:"verificationRecords,omitempty"`
	TrajectorySteps           int      `json:"trajectorySteps,omitempty"`
	BudgetLimitTokens         int64    `json:"budgetLimitTokens,omitempty"`
	BudgetUsedTokens          int64    `json:"budgetUsedTokens,omitempty"`
	BudgetRemainingTokens     int64    `json:"budgetRemainingTokens,omitempty"`
	DeterministicFailureCount int      `json:"deterministicFailureCount,omitempty"`
	ExcludedRuntimeDeps       []string `json:"excludedRuntimeDeps,omitempty"`
	Recommendation            string   `json:"recommendation,omitempty"`
}

func RunReplay(ctx context.Context, opts ReplayOptions) (ReplayReport, error) {
	report := ReplayReport{
		CandidateID: opts.Candidate.ID,
		MinHeldOut:  opts.MinHeldOut,
		Cases:       []ReplayCaseReport{},
	}
	evaluator := opts.Evaluator
	if evaluator == nil {
		evaluator = deterministicReplayEvaluator{}
	}
	for _, bundle := range opts.Bundles {
		if bundle.LowConfidence || bundle.Confidence == ConfidenceLow {
			report.LowConfidenceSkipCount++
			continue
		}
		if err := ctx.Err(); err != nil {
			return report, err
		}
		c, err := evaluator.EvaluateReplay(ctx, opts.Candidate, bundle)
		if c.ID == "" {
			c.ID = bundle.ID
		}
		if c.BundleID == "" {
			c.BundleID = bundle.ID
		}
		if c.CandidateSkillID == "" {
			c.CandidateSkillID = opts.Candidate.ID
		}
		if err != nil {
			c.Error = err.Error()
			c.CandidatePassed = false
		}
		report.Cases = append(report.Cases, c)
	}
	report.ReplayCases = len(report.Cases)
	report.HeldOutCases = len(report.Cases)
	report.InsufficientHeldOut = opts.MinHeldOut > 0 && report.HeldOutCases < opts.MinHeldOut
	summarizeReplay(&report)
	if opts.IncludeHarnessProposal {
		proposal := BuildLongHorizonHarnessProposal(report, opts.Bundles)
		report.HarnessProposal = &proposal
	}
	return report, nil
}

func summarizeReplay(report *ReplayReport) {
	if report == nil || len(report.Cases) == 0 {
		return
	}
	var baselinePassed, candidatePassed int
	for _, c := range report.Cases {
		if c.BaselinePassed {
			baselinePassed++
		}
		if c.CandidatePassed {
			candidatePassed++
		}
		if c.Error != "" {
			report.FailureCount++
		}
		report.BaselineTokens += c.BaselineTokens
		report.CandidateTokens += c.CandidateTokens
	}
	total := float64(len(report.Cases))
	report.BaselinePassRate = float64(baselinePassed) / total
	report.CandidatePassRate = float64(candidatePassed) / total
	if report.BaselineTokens > 0 {
		report.TokenDeltaPercent = (float64(report.CandidateTokens-report.BaselineTokens) / float64(report.BaselineTokens)) * 100
	}
}

func BuildLongHorizonHarnessProposal(report ReplayReport, bundles []Bundle) HarnessProposal {
	proposal := HarnessProposal{
		ProposalID:                "long-horizon-v1",
		DeterministicFailureCount: report.FailureCount,
		ExcludedRuntimeDeps:       []string{"training", "cuda", "vllm", "checkpoint", "model_serving"},
		Recommendation:            "use Maddog replay bundles, curated evidence, verification records, and action/observation trajectories; keep model training and serving stacks out of the app runtime",
	}
	for _, bundle := range bundles {
		if bundle.LowConfidence || bundle.Confidence == ConfidenceLow {
			continue
		}
		proposal.CandidateDocs += len(bundle.CandidateDocs)
		proposal.CuratedEvidence += len(bundle.CuratedEvidence)
		proposal.VerificationRecords += len(bundle.VerificationRecords)
		proposal.TrajectorySteps += len(bundle.Trajectory)
		proposal.BudgetLimitTokens += bundle.BudgetContext.LimitTokens
		proposal.BudgetUsedTokens += bundle.BudgetContext.UsedTokens
		proposal.BudgetRemainingTokens += bundle.BudgetContext.RemainingTokens
	}
	return proposal
}

type deterministicReplayEvaluator struct{}

func (deterministicReplayEvaluator) EvaluateReplay(ctx context.Context, candidate Candidate, bundle Bundle) (ReplayCaseReport, error) {
	if candidate.ID == "" {
		return ReplayCaseReport{}, fmt.Errorf("candidate id is empty")
	}
	return ReplayCaseReport{
		ID:               bundle.ID,
		BundleID:         bundle.ID,
		CandidateSkillID: candidate.ID,
		BaselinePassed:   false,
		CandidatePassed:  candidate.Validation.Valid,
		BaselineTokens:   100,
		CandidateTokens:  80,
		LowConfidence:    bundle.LowConfidence,
	}, nil
}
