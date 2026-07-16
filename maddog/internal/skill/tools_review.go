package skill

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	reviewrules "maddog/internal/review"
	"maddog/internal/secrets"
)

type subagentTaskPreparer func(context.Context, string) (string, error)

var subagentReviewDiff = defaultSubagentReviewDiff

func prepareSubagentReviewTask(ctx context.Context, task string) (string, error) {
	diff, err := subagentReviewDiff(ctx)
	if err != nil {
		return task, nil
	}
	root, _ := os.Getwd()
	report := reviewrules.AnalyzeUnifiedDiff(diff, reviewrules.Options{})
	return reviewrules.BuildTask(diff, task, reviewrules.ChangedFileCodeContext(root, report)), nil
}

func defaultSubagentReviewDiff(ctx context.Context) (string, error) {
	cwd, _ := os.Getwd()
	gitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := runSubagentGit(gitCtx, cwd, "diff", "HEAD")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) != "" {
		return out, nil
	}
	out, err = runSubagentGit(gitCtx, cwd, "diff", "--cached")
	if err != nil {
		return "", err
	}
	return out, nil
}

func runSubagentGit(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(secrets.ProcessEnv(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
