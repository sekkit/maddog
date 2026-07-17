package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	"maddog/internal/repair"
)

func TestSingleInstanceLockRestoresExistingInstance(t *testing.T) {
	app := NewApp()
	lock := singleInstanceLock(app)

	if lock == nil {
		t.Fatal("singleInstanceLock returned nil")
	}
	id := singleInstanceID
	if lock.UniqueId != id {
		t.Fatalf("UniqueId = %q, want %q", lock.UniqueId, id)
	}
	if !strings.HasPrefix(lock.UniqueId, "com.maddog.") {
		t.Fatalf("UniqueId = %q, want Maddog namespace", lock.UniqueId)
	}
	if lock.OnSecondInstanceLaunch == nil {
		t.Fatal("OnSecondInstanceLaunch should restore the existing window")
	}

	lock.OnSecondInstanceLaunch(options.SecondInstanceData{})
}

func TestSecondInstanceLifecycleDoesNotCountAsStartupFailure(t *testing.T) {
	guard := repair.NewStartupGuard(filepath.Join(t.TempDir(), "startup.json"), 3, time.Minute)
	app := NewApp()
	app.startupGuard = guard
	if _, err := guard.Begin(); err != nil {
		t.Fatal(err)
	}
	before := guard.Status()
	lock := singleInstanceLock(app)
	lock.OnSecondInstanceLaunch(options.SecondInstanceData{})
	after := guard.Status()
	if after.ConsecutiveFailures != before.ConsecutiveFailures || after.Phase != before.Phase {
		t.Fatalf("second instance changed startup state: before=%+v after=%+v", before, after)
	}
}

func TestDirectOldInstallPreflightsBeforeWailsWithoutCountingSecondInstance(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "startup.json")
	leasePath := filepath.Join(dir, "desktop.lock")
	first := NewApp()
	first.startupGuard = repair.NewStartupGuard(statePath, 3, time.Minute)
	if !preflightDirectStartup(first, leasePath, false) {
		t.Fatal("first direct launch rejected")
	}
	t.Cleanup(func() { _ = first.startupLease.Release() })
	before := first.startupGuard.Status()
	second := NewApp()
	second.startupGuard = repair.NewStartupGuard(statePath, 3, time.Minute)
	if !preflightDirectStartup(second, leasePath, false) {
		t.Fatal("second direct launch rejected before Wails single-instance handling")
	}
	after := second.startupGuard.Status()
	if after.ConsecutiveFailures != before.ConsecutiveFailures || after.Phase != before.Phase {
		t.Fatalf("second direct launch changed startup state: before=%+v after=%+v", before, after)
	}
}

func TestSingleInstanceLockSkipsInDevMode(t *testing.T) {
	t.Setenv("MADDOG_DEV", "1")
	if lock := singleInstanceLock(NewApp()); lock != nil {
		t.Fatalf("singleInstanceLock returned %#v, want nil in dev mode", lock)
	}
}
