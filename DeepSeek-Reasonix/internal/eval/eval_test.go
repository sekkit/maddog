package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

func TestCaptureWritesAndLoadsReplayBundle(t *testing.T) {
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "fix parser"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	dir := t.TempDir()

	b, path, err := Capture(CaptureOptions{
		SessionID: "abc/123",
		Session:   sess,
		Evidence:  []evidence.Receipt{{ToolName: "bash", Success: false, Command: "go test ./..."}},
		Outcome:   OutcomeInfo{Success: true, GoalMet: true},
		SkillName: "docs",
		Dir:       dir,
		Now:       time.Date(2026, 6, 8, 1, 2, 3, 4, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || !strings.Contains(filepath.Base(path), "abc-123") {
		t.Fatalf("unexpected bundle path %q", path)
	}
	if b.Outcome.FinalAnswer != "done" || b.Outcome.ToolErrors != 1 || b.Outcome.TotalTurns != 1 {
		t.Fatalf("derived outcome fields missing: %+v", b.Outcome)
	}
	loaded, err := LoadBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "abc/123" || loaded.SkillName != "docs" || len(loaded.Messages) != 3 {
		t.Fatalf("loaded bundle = %+v", loaded)
	}
}

func TestReplayRunnerUsesSkillBodyAndReturnsOutcome(t *testing.T) {
	prov := &scriptProvider{turns: []providerTurn{{text: "replayed answer"}}}
	runner := ReplayRunner{Provider: prov}
	out, err := runner.Run(context.Background(), ReplayBundle{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "task"}},
	}, skill.Skill{Name: "docs", Description: "d", Body: "skill system"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success || !out.GoalMet || out.FinalAnswer != "replayed answer" {
		t.Fatalf("outcome = %+v", out)
	}
	if got := prov.requests[0].Messages[0].Content; got != "skill system" {
		t.Fatalf("system prompt = %q, want skill body", got)
	}
}

func TestScoreParsesFrontierAndFallsBack(t *testing.T) {
	frontier := &scriptProvider{turns: []providerTurn{{text: "0.92 strong replay"}}}
	got, err := Score(context.Background(), frontier, OutcomeInfo{Success: true, GoalMet: true}, OutcomeInfo{Success: true, GoalMet: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 0.92 || !strings.Contains(got.Reason, "strong") {
		t.Fatalf("score = %+v", got)
	}
	failing := &scriptProvider{turns: []providerTurn{{err: errors.New("down")}}}
	got, err = Score(context.Background(), failing, OutcomeInfo{Success: true, GoalMet: true}, OutcomeInfo{Success: false})
	if err == nil {
		t.Fatal("expected scorer error with fallback result")
	}
	if got.Score >= 0.7 || !strings.Contains(got.Reason, "fallback") {
		t.Fatalf("fallback score = %+v", got)
	}
}

func TestGuardrailPassesAndRejectsRegressions(t *testing.T) {
	bundles := make([]ReplayBundle, 5)
	oldResults := []OutcomeInfo{
		{Success: true, GoalMet: true}, {Success: true, GoalMet: true}, {Success: true, GoalMet: true}, {Success: false}, {Success: false},
	}
	newResults := []OutcomeInfo{
		{Success: true, GoalMet: true}, {Success: true, GoalMet: true}, {Success: true, GoalMet: true}, {Success: true, GoalMet: true}, {Success: false},
	}
	pass := CheckGuardrail(bundles, oldResults, newResults, []float64{0.8, 0.7, 0.9, 0.75, 0.72}, GuardrailConfig{})
	if !pass.Pass {
		t.Fatalf("guardrail should pass: %+v", pass)
	}
	fail := CheckGuardrail(bundles, newResults, oldResults, []float64{0.8, 0.8, 0.8, 0.8, 0.8}, GuardrailConfig{})
	if fail.Pass || !strings.Contains(fail.Reason, "regression") {
		t.Fatalf("guardrail should reject regression: %+v", fail)
	}
	fail = CheckGuardrail(bundles, oldResults, newResults, []float64{0.8, 0.6, 0.8, 0.8, 0.8}, GuardrailConfig{})
	if fail.Pass || !strings.Contains(fail.Reason, "below") {
		t.Fatalf("guardrail should reject low score: %+v", fail)
	}
}

func TestPromoteWritesSkillAndReturnsEvent(t *testing.T) {
	home := t.TempDir()
	st := skill.New(skill.Options{HomeDir: home, DisableBuiltins: true})
	path, ev, err := Promote(st, skill.Skill{
		Name:         "better-docs",
		Description:  "Better docs",
		Body:         "Use evidence, then write docs.",
		AllowedTools: []string{"read_file"},
	}, skill.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != event.SkillPromoted || !strings.Contains(ev.Text, "better-docs") {
		t.Fatalf("event = %+v", ev)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "allowed-tools: read_file") {
		t.Fatalf("promoted skill missing metadata:\n%s", data)
	}
	if sk, ok := st.Read("better-docs"); !ok || sk.Description != "Better docs" {
		t.Fatalf("promoted skill not readable: %+v ok=%v", sk, ok)
	}
}

type providerTurn struct {
	text string
	err  error
}

type scriptProvider struct {
	turns    []providerTurn
	calls    int
	requests []provider.Request
}

func (p *scriptProvider) Name() string { return "script" }

func (p *scriptProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
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

func TestCaptureDeepCopiesReceipts(t *testing.T) {
	raw := json.RawMessage(`{"command":"go test"}`)
	receipts := []evidence.Receipt{{ToolName: "bash", Args: raw, Success: true, Paths: []string{"a.go"}}}
	b, _, err := Capture(CaptureOptions{Evidence: receipts})
	if err != nil {
		t.Fatal(err)
	}
	receipts[0].ToolName = "mutated"
	receipts[0].Paths[0] = "mutated.go"
	if b.Evidence[0].ToolName != "bash" || b.Evidence[0].Paths[0] != "a.go" {
		t.Fatalf("bundle should copy receipt slice and paths: %+v", b.Evidence[0])
	}
	b.Evidence[0].Args[0] = '{'
	if string(raw) != `{"command":"go test"}` {
		t.Fatalf("bundle should copy receipt raw args, raw = %s", raw)
	}
}
