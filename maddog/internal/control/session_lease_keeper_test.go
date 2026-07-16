package control

import (
	"errors"
	"maddog/internal/agent"
	"maddog/internal/event"
	"path/filepath"
	"testing"
)

func TestSessionLeaseKeeperRebindTransfersAtomically(t *testing.T) {
	d := t.TempDir()
	a := filepath.Join(d, "a.jsonl")
	b := filepath.Join(d, "b.jsonl")
	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.TryAcquireSessionLease(a); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("a acquire=%v", err)
	}
	if err := k.Rebind(b); err != nil {
		t.Fatal(err)
	}
	if got := k.HeldPath(); got != b {
		t.Fatalf("held=%q want %q", got, b)
	}
	l, err := agent.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
}

func TestSessionLeaseKeeperRefusesHeldTargetAndKeepsSource(t *testing.T) {
	d := t.TempDir()
	a := filepath.Join(d, "a.jsonl")
	b := filepath.Join(d, "b.jsonl")
	holder, err := agent.TryAcquireSessionLease(b)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatal(err)
	}
	if err := k.Rebind(b); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("rebind=%v", err)
	}
	if k.HeldPath() != a {
		t.Fatalf("source lease lost")
	}
}

func TestNewCheckedFailsWhenInitialSessionIsLeased(t *testing.T) {
	p := filepath.Join(t.TempDir(), "held.jsonl")
	holder, err := agent.TryAcquireSessionLease(p)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if c, err := NewChecked(Options{SessionPath: p}); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		if c != nil {
			c.Close()
		}
		t.Fatalf("NewChecked err=%v", err)
	}
}

func TestResumeLeaseFailureKeepsCurrentSession(t *testing.T) {
	d := t.TempDir()
	a := filepath.Join(d, "a.jsonl")
	b := filepath.Join(d, "b.jsonl")
	holder, err := agent.TryAcquireSessionLease(b)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	c, err := NewChecked(Options{SessionPath: a})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Resume(agent.NewSession(""), b); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("Resume err=%v", err)
	}
	if got := c.SessionPath(); got != a {
		t.Fatalf("SessionPath=%q want %q", got, a)
	}
}

func TestResumeLeaseFailureKeepsExecutorSession(t *testing.T) {
	d := t.TempDir()
	a := filepath.Join(d, "a.jsonl")
	b := filepath.Join(d, "b.jsonl")
	holder, err := agent.TryAcquireSessionLease(b)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	original := agent.NewSession("original")
	exec := agent.New(nil, nil, original, agent.Options{}, event.Discard)
	c, err := NewChecked(Options{SessionPath: a, Executor: exec})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Resume(agent.NewSession("replacement"), b); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("Resume err=%v", err)
	}
	if got := exec.Session(); got != original {
		t.Fatal("Resume published replacement session despite lease failure")
	}
}
