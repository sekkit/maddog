package skillopt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maddog/internal/evalbench"
	"maddog/internal/provider"
)

func TestProviderProposerConstructsFrozenCandidateAndAccountsUsage(t *testing.T) {
	prov := &proposalProvider{response: `{"candidate_body":"good","edits":[{"start":0,"end":3,"replacement":"good"}],"rationale":"fix failures"}`}
	baseArtifact, err := newArtifact(testSkill("bad"))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := (ProviderProposer{
		Provider: prov, ModelRef: "optimizer/model",
		Pricing: &provider.Pricing{Input: 2, Output: 4},
	}).Propose(context.Background(), ProposalRequest{
		RunID: "run", Round: 1, Base: Revision{Artifact: baseArtifact},
		TrainCases:  []Case{{ID: "train", Input: "do work", Expected: json.RawMessage(`"ok"`)}},
		TrainResult: []EvaluationRecord{{CaseID: "train", Result: Result{Output: "token=secret-value", Soft: 0.1}}},
		Limits:      EditLimits{MaxEdits: 2, MaxChangedBytes: 100, MaxBodyBytes: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Candidate.Body != "good" || !sameFrozenFields(baseArtifact.Skill, proposal.Candidate) || proposal.ModelRef != "optimizer/model" {
		t.Fatalf("proposal = %+v", proposal)
	}
	if proposal.Cost.InputTokens != 10 || proposal.Cost.OutputTokens != 5 || proposal.Cost.Amount <= 0 {
		t.Fatalf("proposal cost = %+v", proposal.Cost)
	}
	if strings.Contains(prov.request.Messages[1].Content, "secret-value") || !strings.Contains(prov.request.Messages[1].Content, "[REDACTED]") {
		t.Fatalf("training trace was not redacted: %s", prov.request.Messages[1].Content)
	}
}

func TestEvalbenchExecutorRejectsUnknownTask(t *testing.T) {
	executor := NewEvalbenchExecutor([]evalbench.Task{{ID: "known"}}, "maddog", "model")
	_, err := executor.Evaluate(context.Background(), RolloutRequest{Case: Case{ID: "missing", Input: "x"}})
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("unknown task error = %v", err)
	}
}

func TestRedactResultRemovesStructuredAndTextSecrets(t *testing.T) {
	evidence := json.RawMessage(`{"api_key":"top-secret","nested":{"message":"Bearer abc.def","image":"data:image/png;base64,AAAA"}}`)
	got := redactResult(Result{Output: "password=hunter2 token:abc", Evidence: evidence})
	serialized := got.Output + string(got.Evidence)
	for _, secret := range []string{"hunter2", "token:abc", "top-secret", "abc.def", "AAAA"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("secret %q remained in %s", secret, serialized)
		}
	}
}

type proposalProvider struct {
	response string
	request  provider.Request
}

func (p *proposalProvider) Name() string { return "proposal-fixture" }

func (p *proposalProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.request = req
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.response}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
