package evalbench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"maddog/internal/skill"
)

func TestLoadSuiteAndPortableGrade(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks", "write-ready")
	mustWrite(t, filepath.Join(taskDir, "task.toml"), `prompt = "write ready"
max_steps = 4
tags = ["local"]
`)
	mustWrite(t, filepath.Join(taskDir, "verify.sh"), `#!/bin/sh
got=$(tr -d '\r\n' < status.txt)
want="ready"
[ "$got" = "$want" ]
`)
	tasks, err := LoadSuite(root)
	if err != nil || len(tasks) != 1 || tasks[0].ID != "write-ready" {
		t.Fatalf("LoadSuite = %+v, %v", tasks, err)
	}
	work := t.TempDir()
	mustWrite(t, filepath.Join(work, "status.txt"), "ready\n")
	hard, soft, err := Grade(work, taskDir)
	if err != nil || hard != 1 || soft != 1 {
		t.Fatalf("Grade = %.2f %.2f %v", hard, soft, err)
	}
}

func TestReadVerifierScoreRejectsMalformedFile(t *testing.T) {
	work := t.TempDir()
	mustWrite(t, filepath.Join(work, "skillopt-score.json"), "not-json")
	if _, _, err := readVerifierScore(work); err == nil || !strings.Contains(err.Error(), "verifier score") {
		t.Fatalf("readVerifierScore malformed score error = %v", err)
	}
}

func TestGradeClearsAgentSuppliedScoreFile(t *testing.T) {
	taskDir := t.TempDir()
	mustWrite(t, filepath.Join(taskDir, "verify.sh"), `#!/bin/sh
got=$(tr -d '\r\n' < status.txt)
want="ready"
[ "$got" = "$want" ]
`)
	work := t.TempDir()
	mustWrite(t, filepath.Join(work, "status.txt"), "ready\n")
	mustWrite(t, filepath.Join(work, "skillopt-score.json"), `{"hard":0,"soft":0}`)
	hard, soft, err := Grade(work, taskDir)
	if err != nil || hard != 1 || soft != 1 {
		t.Fatalf("Grade with agent-supplied score = %.2f %.2f %v", hard, soft, err)
	}
}

func TestRunHidesVerifierAndInstallsSkill(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The fake executable is a cmd file on Windows; exec.Command does not
		// resolve cmd scripts consistently across Go versions in this test.
		t.Skip("portable fake executable is covered on Unix hosts")
	}
	root := t.TempDir()
	taskDir := filepath.Join(root, "case")
	mustWrite(t, filepath.Join(taskDir, "workdir", "seed.txt"), "seed")
	mustWrite(t, filepath.Join(taskDir, "verify.py"), `from pathlib import Path
assert Path("answer.txt").read_text().strip() == "ok"
`)
	bin := filepath.Join(root, "fake-maddog")
	mustWrite(t, bin, `#!/bin/sh
set -eu
test ! -e .skillopt-verify.py
test -f .maddog/skills/check/SKILL.md
printf ok > answer.txt
cat > .skillopt-run-metrics.json <<'EOF'
{"prompt_tokens":10,"completion_tokens":2,"cost":0.25}
EOF
`)
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Task{ID: "x", Prompt: "do it", TimeoutSec: 10, Dir: taskDir}, RunOptions{
		Binary: bin,
		Skill:  skill.Skill{Name: "check", Description: "check things", Body: "Do the work.", RunAs: skill.RunInline},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Hard != 1 || result.Metrics.Cost != 0.25 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunHonorsContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable fake executable is covered on Unix hosts")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "verify.py"), "raise SystemExit(1)\n")
	bin := filepath.Join(root, "slow")
	mustWrite(t, bin, "#!/bin/sh\nsleep 30\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := Run(ctx, Task{ID: "slow", Prompt: "wait", TimeoutSec: 30, Dir: root}, RunOptions{Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(result.RunError), "killed") && !strings.Contains(strings.ToLower(result.RunError), "canceled") {
		t.Fatalf("RunError = %q", result.RunError)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
