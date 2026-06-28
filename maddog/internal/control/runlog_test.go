package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"maddog/internal/event"
	"maddog/internal/loop"
	"maddog/internal/safety"
)

type runLogRunner struct{}

func (runLogRunner) Run(context.Context, string) error { return nil }

type blockingRunLogRunner struct {
	started chan struct{}
}

func (r blockingRunLogRunner) Run(ctx context.Context, _ string) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

type runLogSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *runLogSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *runLogSink) kinds(k event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []event.Event{}
	for _, e := range s.events {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

func TestRunWritesLoopRunLogAndReport(t *testing.T) {
	dir := t.TempDir()
	sink := &runLogSink{}
	log := loop.NewRunLog(loop.RunLogOptions{
		Path:     filepath.Join(dir, "run.jsonl"),
		Redactor: safety.DefaultRedactor(),
	})
	ctrl := New(Options{
		Runner: runLogRunner{},
		Sink:   sink,
		RunLog: log,
		Label:  "coding-task",
	})

	if err := ctrl.Run(context.Background(), "hello Authorization: Bearer sk-input-secret"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "run.jsonl"))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	out := string(body)
	if !strings.Contains(out, string(loop.RunEventStarted)) || !strings.Contains(out, string(loop.RunEventStopped)) || !strings.Contains(out, string(loop.RunEventReportReady)) {
		t.Fatalf("run log missing lifecycle events:\n%s", out)
	}
	if strings.Contains(out, "sk-input-secret") {
		t.Fatalf("run log leaked prompt secret:\n%s", out)
	}
	reports := sink.kinds(event.RunReportReady)
	if len(reports) != 1 || reports[0].RunReport == nil || reports[0].RunReport.Status != "completed" {
		t.Fatalf("run report events = %+v", reports)
	}
	if reports[0].RunReport.ReportPath != filepath.Join(dir, "report.json") {
		t.Fatalf("run report path = %+v", reports[0].RunReport)
	}
	if _, err := os.Stat(reports[0].RunReport.ReportPath); err != nil {
		t.Fatalf("report file not written: %v", err)
	}
}

func TestCancelWritesKillSwitchRunLogEvent(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{})
	log := loop.NewRunLog(loop.RunLogOptions{
		Path:     filepath.Join(dir, "run.jsonl"),
		Redactor: safety.DefaultRedactor(),
	})
	ctrl := New(Options{
		Runner: blockingRunLogRunner{started: started},
		RunLog: log,
		Label:  "coding-task",
	})

	ctrl.Send("stop me")
	<-started
	ctrl.Cancel()

	path := filepath.Join(dir, "run.jsonl")
	var body []byte
	var err error
	found := false
	deadline := time.Now().Add(time.Second)
	for {
		body, err = os.ReadFile(path)
		if err == nil && strings.Contains(string(body), string(loop.RunEventKillSwitchTriggered)) {
			found = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	if !strings.Contains(string(body), string(loop.RunEventKillSwitchTriggered)) {
		t.Fatalf("run log missing kill switch event:\n%s", string(body))
	}
	if found {
		deadline = time.Now().Add(time.Second)
		for ctrl.Running() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if ctrl.Running() {
			t.Fatal("controller did not stop after cancel")
		}
	}
}
