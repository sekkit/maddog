package processlock

import (
	"path/filepath"
	"testing"
)

func TestTryIsExclusiveAndStaleFileIsReusable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.lock")
	first, acquired, err := Try(path)
	if err != nil || !acquired {
		t.Fatalf("first lock: acquired=%v err=%v", acquired, err)
	}
	if second, acquired, err := Try(path); err != nil || acquired || second != nil {
		t.Fatalf("second lock: lock=%v acquired=%v err=%v", second, acquired, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	third, acquired, err := Try(path)
	if err != nil || !acquired {
		t.Fatalf("reused stale lock file: acquired=%v err=%v", acquired, err)
	}
	if err := third.Release(); err != nil {
		t.Fatal(err)
	}
}
