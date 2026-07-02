package main

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
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

func TestSingleInstanceLockSkipsInDevMode(t *testing.T) {
	t.Setenv("MADDOG_DEV", "1")
	if lock := singleInstanceLock(NewApp()); lock != nil {
		t.Fatalf("singleInstanceLock returned %#v, want nil in dev mode", lock)
	}
}
