package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func TestDeliveryLifecycle(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "config", "user.email", "test@example.invalid")
	gitTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "init")
	state := filepath.Join(t.TempDir(), "state")
	m, err := NewManager(root, state)
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Create(context.Background(), "maddog/test-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if d.Path == root || !strings.HasPrefix(d.Path, state+string(filepath.Separator)) {
		t.Fatalf("unexpected isolated path %q", d.Path)
	}
	if err := os.WriteFile(filepath.Join(d.Path, "change"), []byte("delivery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ins, err := m.Inspect(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ins.Dirty || !ins.Ready {
		t.Fatalf("inspection = %+v", ins)
	}
	if err := m.Apply(context.Background(), d.ID); err == nil {
		t.Fatal("apply should reject uncommitted delivery")
	}
	gitTest(t, d.Path, "add", ".")
	gitTest(t, d.Path, "commit", "-qm", "delivery")
	if err := m.Apply(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitTest(t, root, "log", "-1", "--pretty=%s")); got != "delivery" {
		t.Fatalf("base log = %q", got)
	}
}

func TestLeaseAndDiscardPreserveState(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "config", "user.email", "test@example.invalid")
	gitTest(t, root, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644)
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "init")
	state := filepath.Join(t.TempDir(), "state")
	m, _ := NewManager(root, state)
	d, err := m.Create(context.Background(), "maddog/discard")
	if err != nil {
		t.Fatal(err)
	}
	if err := acquireLease(state, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Discard(context.Background(), d.ID); err != ErrLeaseHeld {
		t.Fatalf("discard with lease = %v", err)
	}
	releaseLease(state, d.ID)
	if err := m.Discard(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(d.ID); err != ErrNotFound {
		t.Fatalf("open discarded = %v", err)
	}
}

func TestShortPathIsStableAndBounded(t *testing.T) {
	p := ShortPath(filepath.Join(t.TempDir(), "state"), strings.Repeat("x", 1000))
	if len(filepath.Base(p)) != 12 {
		t.Fatalf("path %q", p)
	}
}
