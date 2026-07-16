package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeModePolicyDisablesExternalRuntime(t *testing.T) {
	p := SafeModePolicy()
	for _, capability := range []Capability{CapabilityWebview, CapabilityPlugins, CapabilityMCP, CapabilityHooks, CapabilityBots, CapabilitySessions, CapabilitySkills, CapabilitySidecars, CapabilityMemoryLearning, CapabilityModelUpgrades, CapabilityNetwork} {
		if p.Allows(capability) {
			t.Errorf("safe mode allows %s", capability)
		}
	}
	for _, capability := range []Capability{CapabilityBuiltinConfig, CapabilityManualApproval, CapabilitySandbox} {
		if !p.Allows(capability) {
			t.Errorf("safe mode blocks %s", capability)
		}
	}
}

func TestProcessGateIsLocalAndDefaultsToNormal(t *testing.T) {
	normal := NewProcessGate(false)
	if !normal.Allows(CapabilityNetwork) {
		t.Fatal("normal process should allow network")
	}
	safe := NewProcessGate(true)
	if safe.Allows(CapabilityNetwork) || !safe.Allows(CapabilitySandbox) {
		t.Fatal("safe process policy mismatch")
	}
	if normal.Allows(CapabilityNetwork) != true {
		t.Fatal("safe mode leaked into another process gate")
	}
}

func TestStartupGuardEntersSafeModeAfterCrashLoopAndRecovers(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	g := NewStartupGuard(filepath.Join(dir, "startup.json"), 3, time.Minute)
	g.now = func() time.Time { return clock }
	for i := 0; i < 2; i++ {
		inSafe, err := g.Begin()
		if err != nil || inSafe {
			t.Fatalf("launch %d: safe=%v err=%v", i, inSafe, err)
		}
	}
	inSafe, err := g.Begin()
	if err != nil || !inSafe {
		t.Fatalf("third failed launch: safe=%v err=%v", inSafe, err)
	}
	if err := g.MarkHealthy(); err != nil {
		t.Fatal(err)
	}
	inSafe, err = g.Begin()
	if err != nil || inSafe {
		t.Fatalf("healthy launch should leave probation: safe=%v err=%v", inSafe, err)
	}
}

func TestStartupGuardReadyDoesNotClearCrashCount(t *testing.T) {
	g := NewStartupGuard(filepath.Join(t.TempDir(), "startup.json"), 2, time.Minute)
	if safe, err := g.Begin(); err != nil || safe {
		t.Fatalf("begin safe=%v err=%v", safe, err)
	}
	if err := g.MarkReady(); err != nil {
		t.Fatal(err)
	}
	if safe, err := g.Begin(); err != nil || !safe {
		t.Fatalf("ready launch must still count as failed: safe=%v err=%v", safe, err)
	}
}

func TestRepairerReplacesAndRollsBackOnlyOwnedPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRepairer(root)
	receipt, err := r.Replace("config/settings.json", []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("got %q", got)
	}
	if err := r.Rollback(receipt); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "old" {
		t.Fatalf("rollback got %q", got)
	}
	if _, err := r.Replace(filepath.Join("..", "outside"), []byte("x")); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Fatalf("outside path accepted: %v", err)
	}
	if err := r.Rollback(Receipt{Target: filepath.Join(root, "..", "outside"), Existed: false}); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Fatalf("forged rollback target accepted: %v", err)
	}
}

func TestUpdateTransactionStagesVerifiesAndCancels(t *testing.T) {
	dir := t.TempDir()
	tx := NewUpdateTransaction(dir)
	if err := tx.Prepare([]byte("release"), ""); err == nil {
		t.Fatal("missing digest accepted")
	}
	digest := SHA256([]byte("release"))
	if err := tx.Prepare([]byte("release"), digest); err != nil {
		t.Fatal(err)
	}
	if err := tx.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "staged")); !os.IsNotExist(err) {
		t.Fatalf("staged update remains: %v", err)
	}
	first := NewUpdateTransaction(dir)
	if err := first.Prepare([]byte("release"), digest); err != nil {
		t.Fatal(err)
	}
	second := NewUpdateTransaction(dir)
	if err := second.Prepare([]byte("release"), digest); err == nil {
		t.Fatal("second updater transaction acquired the lock")
	}
	if err := first.Cancel(); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(dir, "current")
	if err := os.WriteFile(current, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx2 := NewUpdateTransaction(filepath.Join(dir, "tx"))
	if err := tx2.Prepare([]byte("new"), SHA256([]byte("new"))); err != nil {
		t.Fatal(err)
	}
	if err := tx2.CommitWithRollback(current, func(staged, _ string) error {
		if err := os.WriteFile(current, []byte("corrupt"), 0o600); err != nil {
			return err
		}
		return os.ErrPermission
	}); err == nil {
		t.Fatal("failed install unexpectedly committed")
	}
	got, _ := os.ReadFile(current)
	if string(got) != "old" {
		t.Fatalf("rollback got %q", got)
	}
}

func TestUpdateTransactionReverifiesStagedDigestAtCommit(t *testing.T) {
	dir := t.TempDir()
	tx := NewUpdateTransaction(dir)
	if err := tx.Prepare([]byte("verified"), SHA256([]byte("verified"))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := tx.Commit(func(string) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("tampered release applied: called=%v err=%v", called, err)
	}
	_ = tx.Cancel()
}
