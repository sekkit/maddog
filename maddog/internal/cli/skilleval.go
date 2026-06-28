package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"maddog/internal/skilleval"
)

func skillEvalCommand(args []string) int {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list":
		return skillEvalList(argsAfterSubcommand(args))
	case "evaluate", "eval":
		return skillEvalEvaluate(argsAfterSubcommand(args))
	case "help", "-h", "--help":
		skillEvalUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown skilleval subcommand %q\n\n", sub)
		skillEvalUsage()
		return 2
	}
}

func skillEvalList(args []string) int {
	fs := flag.NewFlagSet("skilleval list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print candidates as JSON")
	dir := fs.String("dir", "", "project root; reads .maddog/skilleval from this directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	store := skilleval.NewProjectStore(workflowRoot(*dir))
	candidates, err := store.ListCandidates()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(candidates)
	}
	fmt.Println("maddog skilleval candidates")
	for _, candidate := range candidates {
		decision := "-"
		if candidate.Evaluation != nil && candidate.Evaluation.Decision != "" {
			decision = candidate.Evaluation.Decision
		}
		fmt.Printf("%-22s %-12s %-24s %s\n", candidate.ID, candidate.Status, candidate.Skill.Name, decision)
	}
	return 0
}

func skillEvalEvaluate(args []string) int {
	fs := flag.NewFlagSet("skilleval evaluate", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print evaluation as JSON")
	dir := fs.String("dir", "", "project root; reads .maddog/skilleval from this directory")
	minHeldOut := fs.Int("min-held-out", 2, "minimum high-confidence replay bundles required")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: maddog skilleval evaluate [--dir PATH] [--json] [--min-held-out N] <candidate-id>")
		return 2
	}
	store := skilleval.NewProjectStore(workflowRoot(*dir))
	candidate, ok, err := store.ReadCandidate(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "candidate %q not found\n", rest[0])
		return 1
	}
	bundles, err := candidateBundles(store, candidate)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	replay, err := skilleval.RunReplay(context.Background(), skilleval.ReplayOptions{
		Candidate:  candidate,
		Bundles:    bundles,
		MinHeldOut: *minHeldOut,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	guard := skilleval.EvaluateGuardrails(candidate, skilleval.GuardrailOptions{Replay: replay, MinHeldOut: *minHeldOut})
	score := guard
	if guard.Decision != skilleval.DecisionRejected {
		score, err = skilleval.ScorePromotion(context.Background(), replay, skilleval.ScoreOptions{MinHeldOut: *minHeldOut})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if guard.Decision == skilleval.DecisionReviewNeeded && score.Decision == skilleval.DecisionPromotable {
			score = guard
		}
	}
	summary := skilleval.EvaluationSummary{
		CandidateID:         candidate.ID,
		Decision:            string(score.Decision),
		Score:               score.Score,
		Reason:              score.Reason,
		ReplayCases:         replay.ReplayCases,
		HeldOutCases:        replay.HeldOutCases,
		BaselinePassRate:    replay.BaselinePassRate,
		CandidatePassRate:   replay.CandidatePassRate,
		TokenDeltaPercent:   replay.TokenDeltaPercent,
		FrontierUnavailable: score.FrontierUnavailable,
		EvaluatedAt:         time.Now().UTC(),
	}
	if _, err := store.UpdateCandidateEvaluation(candidate.ID, summary); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(summary)
	}
	fmt.Printf("%s %s score=%.2f held_out=%d/%d reason=%s\n", candidate.ID, summary.Decision, summary.Score, summary.HeldOutCases, *minHeldOut, summary.Reason)
	return 0
}

func candidateBundles(store *skilleval.Store, candidate skilleval.Candidate) ([]skilleval.Bundle, error) {
	var bundles []skilleval.Bundle
	for _, id := range candidate.BundleIDs {
		bundle, ok, err := store.ReadBundle(id)
		if err != nil {
			return nil, err
		}
		if ok {
			bundles = append(bundles, bundle)
		}
	}
	if len(bundles) == 0 {
		return nil, fmt.Errorf("candidate %q has no available replay bundles", candidate.ID)
	}
	return bundles, nil
}

func writeJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func skillEvalUsage() {
	fmt.Print(`maddog skilleval - inspect and evaluate generated skill candidates

Usage:
  maddog skilleval list [--dir PATH] [--json]
  maddog skilleval evaluate [--dir PATH] [--json] [--min-held-out N] <candidate-id>

Candidates and replay bundles are read from .maddog/skilleval under the selected directory.
`)
}
