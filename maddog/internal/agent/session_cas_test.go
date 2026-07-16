package agent

import (
	"errors"
	"maddog/internal/provider"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionSaveRejectsDiskTamperWithoutRevisionChange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{\"role\":\"user\",\"content\":\"tampered\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	if err := loaded.Save(p); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("Save err=%v, want conflict", err)
	}
}

func TestSessionSaveFailsClosedOnCorruptSidecar(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BranchMetaPath(p), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "next"})
	if err := s.Save(p); err == nil {
		t.Fatal("Save succeeded with corrupt sidecar")
	}
}

func TestSessionSaveFailsClosedOnCorruptTranscriptWithoutSidecar(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	corrupt := []byte("{not-json}\n")
	if err := os.WriteFile(p, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "local work"})
	if err := s.Save(p); err == nil {
		t.Fatal("Save succeeded with corrupt non-empty transcript")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("corrupt transcript overwritten: got %q want %q", got, corrupt)
	}
	branches, err := filepath.Glob(filepath.Join(filepath.Dir(p), "session.recovery-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 {
		t.Fatalf("recovery branches=%v, want one", branches)
	}
}

func TestBranchMetaUpdatePreservesTranscriptCASFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "base"})
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	before, ok, err := LoadBranchMeta(p)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if err := SaveBranchMeta(p, BranchMeta{ID: before.ID, Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	after, _, err := LoadBranchMeta(p)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.ContentDigest != before.ContentDigest || after.WriterID != before.WriterID {
		t.Fatalf("CAS fields changed: before=%+v after=%+v", before, after)
	}
}

func TestSessionSaveRejectsStaleSnapshotAndCreatesRecoveryBranch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	first := NewSession("")
	first.Add(provider.Message{Role: provider.RoleUser, Content: "disk"})
	if err := first.Save(p); err != nil {
		t.Fatal(err)
	}
	stale := NewSession("")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local"})
	if err := stale.Save(p); !errors.Is(err, ErrSessionSnapshotConflict) {
		t.Fatalf("Save err=%v, want conflict", err)
	}
	branches, err := filepath.Glob(filepath.Join(filepath.Dir(p), "session.recovery-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 {
		t.Fatalf("recovery branches=%v, want one", branches)
	}
}

func TestSessionLeaseExclusiveAndReleases(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	one, err := TryAcquireSessionLease(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryAcquireSessionLease(p); !errors.Is(err, ErrSessionLeaseHeld) {
		t.Fatalf("second lease err=%v", err)
	}
	one.Release()
	two, err := TryAcquireSessionLease(p)
	if err != nil {
		t.Fatal(err)
	}
	two.Release()
}
