package skillopt

// StrictGate is lexicographic: hard-verifier performance is primary and may
// never regress outside the deadband. When hard performance is tied, soft
// quality must clear both the deadband and minimum improvement threshold.
type StrictGate struct{}

func (StrictGate) Decide(in GateInput) Decision {
	decision := Decision{
		CaseIDs:   append([]string(nil), in.CaseIDs...),
		Baseline:  aggregatePairs(in.Pairs, func(p PairedResult) Result { return p.Baseline }),
		Current:   aggregatePairs(in.Pairs, func(p PairedResult) Result { return p.Current }),
		Candidate: aggregatePairs(in.Pairs, func(p PairedResult) Result { return p.Candidate }),
	}
	decision.HardDelta = decision.Candidate.HardRate - decision.Current.HardRate
	decision.SoftDelta = decision.Candidate.SoftMean - decision.Current.SoftMean

	switch {
	case len(in.Pairs) == 0:
		decision.Reason = "no paired validation cases"
		decision.DecisiveMetric = "none"
	case decision.HardDelta < -in.Deadband:
		decision.Reason = "hard score regressed"
		decision.DecisiveMetric = "hard"
	case decision.HardDelta > in.Deadband:
		decision.DecisiveMetric = "hard"
		if decision.HardDelta < in.MinDelta {
			decision.Reason = "hard improvement below min_delta"
		} else {
			decision.Accepted = true
			decision.Reason = "hard improvement accepted"
		}
	case decision.SoftDelta <= in.Deadband:
		decision.Reason = "change is inside deadband"
		decision.DecisiveMetric = "soft"
	case decision.SoftDelta < in.MinDelta:
		decision.Reason = "soft improvement below min_delta"
		decision.DecisiveMetric = "soft"
	default:
		decision.Accepted = true
		decision.Reason = "soft improvement accepted"
		decision.DecisiveMetric = "soft"
	}
	return decision
}

func aggregatePairs(pairs []PairedResult, pick func(PairedResult) Result) Aggregate {
	if len(pairs) == 0 {
		return Aggregate{}
	}
	var aggregate Aggregate
	aggregate.Cases = len(pairs)
	for _, pair := range pairs {
		result := pick(pair)
		if result.Hard {
			aggregate.HardRate++
		}
		aggregate.SoftMean += result.Soft
		aggregate.CostTotal.InputTokens += result.Cost.InputTokens
		aggregate.CostTotal.OutputTokens += result.Cost.OutputTokens
		aggregate.CostTotal.Amount += result.Cost.Amount
	}
	aggregate.HardRate /= float64(len(pairs))
	aggregate.SoftMean /= float64(len(pairs))
	return aggregate
}
