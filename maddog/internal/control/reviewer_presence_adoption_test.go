package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/event"
	"maddog/internal/guardian"
	"maddog/internal/provider"
	"maddog/internal/tool"
)

type unavailableReviewerProvider struct{ requests int }

func (p *unavailableReviewerProvider) Name() string { return "reviewer-down" }
func (p *unavailableReviewerProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.requests++
	return nil, fmt.Errorf("reviewer transport down")
}

func TestReviewerFailureRequiresPresenceEveryInvocation(t *testing.T) {
	reviewer := &unavailableReviewerProvider{}
	guardianSession := guardian.NewSession(reviewer, tool.NewRegistry(), guardian.PolicyPrompt(), "test", 0, nil, event.Discard)
	executor := agent.New(&recordingProvider{name: "executor"}, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	approvals := make(chan event.Approval, 2)
	controller := New(Options{
		Executor: executor,
		Guardian: guardianSession,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
	})
	type result struct {
		allow  bool
		reason string
		err    error
	}
	run := func() <-chan result {
		done := make(chan result, 1)
		go func() {
			allow, _, reason, err := (gateApprover{controller}).ApproveWithReason(context.Background(), "write_file", "main.go", json.RawMessage(`{"path":"main.go"}`))
			done <- result{allow: allow, reason: reason, err: err}
		}()
		return done
	}

	for invocation := 1; invocation <= 2; invocation++ {
		done := run()
		var approval event.Approval
		select {
		case approval = <-approvals:
		case <-time.After(5 * time.Second):
			t.Fatalf("invocation %d did not require a present user", invocation)
		}
		if !strings.Contains(approval.Reason, "reviewer was unavailable") {
			t.Fatalf("approval reason = %q", approval.Reason)
		}
		controller.Approve(approval.ID, true, true, true)
		select {
		case got := <-done:
			if got.err != nil || !got.allow {
				t.Fatalf("invocation %d result = %+v", invocation, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("invocation %d did not finish", invocation)
		}
	}
	if reviewer.requests != 2 {
		t.Fatalf("reviewer requests = %d, want 2", reviewer.requests)
	}
}
