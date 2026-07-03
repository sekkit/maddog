package skilleval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/skill"
)

func TestReplayRunnerBuildsReadOnlyPromptAndReturnsOutcome(t *testing.T) {
	prov := &scriptReplayProvider{turns: []providerTurn{{text: "replayed answer"}}}
	runner := ReplayRunner{Provider: prov}
	candidate := Candidate{Hash: "abc", Skill: validSkill("parser-helper"), Status: CandidatePending}
	out, err := runner.Run(context.Background(), BundleV2{
		ID:   "bundle-a",
		Task: "fix parser",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "fix parser"},
			{Role: provider.RoleAssistant, Content: "parser failed"},
		},
		Evidence: []evidence.Receipt{{ToolName: "go test", Success: false, Command: "go test ./..."}},
		Outcome:  OutcomeInfo{Success: false, ToolErrors: 1},
	}, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success || !out.GoalMet || out.FinalAnswer != "replayed answer" || out.TotalTurns != 1 {
		t.Fatalf("outcome = %+v", out)
	}
	if out.Confidence == OutcomeConfidenceVerified {
		t.Fatalf("provider replay completion should not self-verify outcome: %+v", out)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.calls)
	}
	got := prov.requests[0]
	if !strings.Contains(got.Messages[0].Content, "read-only replay") || !strings.Contains(got.Messages[0].Content, "Use the parser checklist") {
		t.Fatalf("system prompt missing read-only candidate skill context: %+v", got.Messages)
	}
	if !strings.Contains(got.Messages[1].Content, "bundle-a") || !strings.Contains(got.Messages[1].Content, "go test ./...") {
		t.Fatalf("user prompt missing bundle/evidence: %+v", got.Messages)
	}
}

func TestReplayRunnerRejectsRejectedCandidate(t *testing.T) {
	runner := ReplayRunner{Provider: &scriptReplayProvider{turns: []providerTurn{{text: "unused"}}}}
	_, err := runner.Run(context.Background(), BundleV2{ID: "bundle-a"}, Candidate{Hash: "abc", Status: CandidateRejected})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("Run rejected candidate err = %v", err)
	}
}

func TestDryRunReplayUsesCandidateBody(t *testing.T) {
	out := DryRunReplay(BundleV2{Outcome: OutcomeInfo{Success: true, GoalMet: true}}, Candidate{
		Skill:  validSkill("parser-helper"),
		Status: CandidatePending,
	})
	if !out.Success || !out.GoalMet || !strings.Contains(out.FinalAnswer, "Use the parser checklist") {
		t.Fatalf("dry-run outcome = %+v, want candidate body reflected", out)
	}
}

type providerTurn struct {
	text string
	err  error
}

type scriptReplayProvider struct {
	turns    []providerTurn
	calls    int
	requests []provider.Request
}

func (p *scriptReplayProvider) Name() string { return "script" }

func (p *scriptReplayProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	p.requests = append(p.requests, req)
	if p.calls > len(p.turns) {
		return nil, errors.New("no scripted turn")
	}
	turn := p.turns[p.calls-1]
	if turn.err != nil {
		return nil, turn.err
	}
	ch := make(chan provider.Chunk, 2)
	go func() {
		defer close(ch)
		if turn.text != "" {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: turn.text}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func validScoredSkill(name string, tools ...string) skill.Skill {
	sk := validSkill(name)
	sk.AllowedTools = tools
	return sk
}
