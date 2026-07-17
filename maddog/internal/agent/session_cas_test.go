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

func TestRecoveryBranchesDeduplicateAndRetainNewestFive(t *testing.T) {
	if MaxRecoveryBranches < 1 {
		t.Fatalf("MaxRecoveryBranches=%d must preserve at least one complete snapshot", MaxRecoveryBranches)
	}
	p := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "same"})
	one, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "first"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "duplicate"})
	if err != nil {
		t.Fatal(err)
	}
	if two.Path != one.Path {
		t.Fatalf("duplicate recovery path=%q, want %q", two.Path, one.Path)
	}

	for i := 0; i < MaxRecoveryBranches; i++ {
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: string(rune('a' + i))})
		if _, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "distinct"}); err != nil {
			t.Fatal(err)
		}
	}
	branches, err := filepath.Glob(filepath.Join(filepath.Dir(p), "session.recovery-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != MaxRecoveryBranches {
		t.Fatalf("recovery branches=%d, want %d: %v", len(branches), MaxRecoveryBranches, branches)
	}
	if _, err := os.Stat(one.Path); !os.IsNotExist(err) {
		t.Fatalf("oldest duplicate branch retained: err=%v", err)
	}
	for _, branch := range branches {
		if _, ok, err := LoadBranchMeta(branch); err != nil || !ok {
			t.Fatalf("branch meta %s ok=%v err=%v", branch, ok, err)
		}
	}
}

func TestSaveRecoveryBranchReconcilesOrphanedPairs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	transcriptOnly := filepath.Join(dir, "session.recovery-1.jsonl")
	if err := os.WriteFile(transcriptOnly, []byte("{\"role\":\"user\",\"content\":\"orphan\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadataOnly := filepath.Join(dir, "session.recovery-2.jsonl")
	if err := SaveBranchMeta(metadataOnly, BranchMeta{ID: BranchID(metadataOnly), Recovered: true}); err != nil {
		t.Fatal(err)
	}

	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "recover me"})
	info, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "conflict"})
	if err != nil {
		t.Fatal(err)
	}

	for _, orphan := range []string{transcriptOnly, BranchMetaPath(transcriptOnly), metadataOnly, BranchMetaPath(metadataOnly)} {
		if _, err := os.Stat(orphan); !os.IsNotExist(err) {
			t.Fatalf("orphan artifact %q retained: %v", orphan, err)
		}
	}
	transcripts, err := filepath.Glob(filepath.Join(dir, "session.recovery-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(transcripts) != 1 || transcripts[0] != info.Path {
		t.Fatalf("recovery transcripts = %v, want complete pair for %q", transcripts, info.Path)
	}
	if _, ok, err := LoadBranchMeta(info.Path); err != nil || !ok {
		t.Fatalf("published recovery metadata ok=%v err=%v", ok, err)
	}
}

func TestSaveRecoveryBranchReconcilesPairAfterEitherArtifactDisappears(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "recover me"})
	first, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	second, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "transcript lost"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Path == first.Path {
		t.Fatalf("metadata-only recovery reused as complete pair: %q", second.Path)
	}
	if _, err := os.Stat(BranchMetaPath(first.Path)); !os.IsNotExist(err) {
		t.Fatalf("orphan metadata retained: %v", err)
	}

	if err := os.Remove(BranchMetaPath(second.Path)); err != nil {
		t.Fatal(err)
	}
	third, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "metadata lost"})
	if err != nil {
		t.Fatal(err)
	}
	if third.Path == second.Path {
		t.Fatalf("transcript-only recovery reused as complete pair: %q", third.Path)
	}
	if _, err := os.Stat(second.Path); !os.IsNotExist(err) {
		t.Fatalf("orphan transcript retained: %v", err)
	}

	duplicate, err := s.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "repeat"})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Path != third.Path {
		t.Fatalf("complete recovery pair not deduplicated: got %q want %q", duplicate.Path, third.Path)
	}
}

func TestSaveRecoveryBranchCleansPartialArtifactsWhenTranscriptWriteFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "recover me"})
	wantErr := errors.New("injected transcript write failure")
	var writtenPath string

	_, err := s.saveRecoveryBranch(RecoveryBranchOptions{OriginalPath: p, Reason: "write failed"}, func(path string, _ []provider.Message) error {
		writtenPath = path
		if err := os.WriteFile(path, []byte("partial\n"), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(BranchMetaPath(path), []byte("partial\n"), 0o600); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("SaveRecoveryBranch error = %v, want %v", err, wantErr)
	}
	for _, artifact := range []string{writtenPath, BranchMetaPath(writtenPath)} {
		if _, err := os.Stat(artifact); !os.IsNotExist(err) {
			t.Fatalf("partial artifact %q retained: %v", artifact, err)
		}
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
