package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDeliveryWorktreeBindingsExposeExplicitLifecycle(t *testing.T) {
	repo := t.TempDir()
	gitDeliveryTest(t, repo, "init")
	gitDeliveryTest(t, repo, "config", "user.email", "maddog@example.test")
	gitDeliveryTest(t, repo, "config", "user.name", "Maddog Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitDeliveryTest(t, repo, "add", "README.md")
	gitDeliveryTest(t, repo, "commit", "-m", "base")

	app := NewApp()
	app.deliveryStateRoot = t.TempDir()
	app.openDeliveryPath = func(string) error { return nil }
	app.tabs["project"] = &WorkspaceTab{ID: "project", Scope: "project", WorkspaceRoot: repo}
	app.activeTabID = "project"

	delivery, err := app.CreateDeliveryWorktree("maddog/delivery/ui-test")
	if err != nil {
		t.Fatal(err)
	}
	if delivery.BasePath != repo || delivery.Path == "" {
		t.Fatalf("unexpected delivery: %+v", delivery)
	}
	listed, err := app.ListDeliveryWorktrees()
	if err != nil || len(listed) != 1 || listed[0].ID != delivery.ID {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	opened, err := app.OpenDeliveryWorktree(delivery.ID)
	if err != nil || opened.Path != delivery.Path {
		t.Fatalf("open = %+v, %v", opened, err)
	}
	inspection, err := app.InspectDeliveryWorktree(delivery.ID)
	if err != nil || !inspection.Ready || inspection.Delivery.ID != delivery.ID {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
	if err := app.DiscardDeliveryWorktree(delivery.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = app.ListDeliveryWorktrees()
	if err != nil || len(listed) != 1 || listed[0].State != "discarded" {
		t.Fatalf("discarded list = %+v, %v", listed, err)
	}

	applied, err := app.CreateDeliveryWorktree("maddog/delivery/apply-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(applied.Path, "applied.txt"), []byte("explicit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitDeliveryTest(t, applied.Path, "add", "applied.txt")
	gitDeliveryTest(t, applied.Path, "commit", "-m", "delivery change")
	if err := app.ApplyDeliveryWorktree(applied.ID); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(repo, "applied.txt")); err != nil || string(data) != "explicit\n" {
		t.Fatalf("applied file = %q, %v", data, err)
	}
}

func gitDeliveryTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
