// Package evalbench runs Maddog benchmark cases in disposable workspaces and
// grades them with verifiers that are hidden from the agent during rollout.
package evalbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"maddog/internal/skill"
)

const traceLimit = 64 << 10

// Task is one deterministic benchmark case. Dir contains task.toml,
// verify.sh (or verify.py), and an optional workdir seed.
type Task struct {
	ID         string   `json:"id"`
	Prompt     string   `toml:"prompt" json:"prompt"`
	MaxSteps   int      `toml:"max_steps" json:"max_steps,omitempty"`
	TimeoutSec int      `toml:"timeout_sec" json:"timeout_sec,omitempty"`
	Tags       []string `toml:"tags" json:"tags,omitempty"`
	Requires   []string `toml:"requires" json:"requires,omitempty"`
	Dir        string   `toml:"-" json:"dir"`
}

// Metrics mirrors the stable subset emitted by `maddog run --metrics`.
type Metrics struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Steps            int     `json:"steps"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	ToolCalls        int     `json:"tool_calls"`
	ToolErrors       int     `json:"tool_errors"`
}

// Result is the complete outcome used by skill optimization.
type Result struct {
	CaseID     string        `json:"case_id"`
	Hard       float64       `json:"hard"`
	Soft       float64       `json:"soft"`
	Passed     bool          `json:"passed"`
	Trace      string        `json:"trace,omitempty"`
	GradeError string        `json:"grade_error,omitempty"`
	RunError   string        `json:"run_error,omitempty"`
	Metrics    Metrics       `json:"metrics"`
	Duration   time.Duration `json:"duration"`
	Workspace  string        `json:"workspace,omitempty"`
}

// RunOptions configures one isolated target-agent rollout.
type RunOptions struct {
	Binary        string
	Model         string
	Skill         skill.Skill
	SkillMarkdown string
	// ProjectConfig is copied into the disposable workspace as maddog.toml.
	// It contains provider/runtime configuration but no secret values; API keys
	// remain environment references and offline mode suppresses hooks/plugins.
	ProjectConfig string
	KeepWorkspace bool
	Environment   []string
}

// LoadSuite reads <root>/tasks/<id>/task.toml in deterministic ID order.
func LoadSuite(root string) ([]Task, error) {
	tasksDir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, entry.Name())
		var task Task
		if _, err := toml.DecodeFile(filepath.Join(dir, "task.toml"), &task); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		task.ID = entry.Name()
		task.Dir = dir
		if task.TimeoutSec <= 0 {
			task.TimeoutSec = 240
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// Run executes one case. The verifier is copied only after the agent exits.
func Run(ctx context.Context, task Task, opts RunOptions) (Result, error) {
	started := time.Now()
	result := Result{CaseID: task.ID}
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Prompt) == "" {
		return result, fmt.Errorf("benchmark task requires id and prompt")
	}
	if strings.TrimSpace(task.Dir) == "" {
		return result, fmt.Errorf("benchmark task %s has no source directory", task.ID)
	}
	bin := strings.TrimSpace(opts.Binary)
	if bin == "" {
		bin = "maddog"
	}
	if strings.ContainsAny(bin, `/\`) && !filepath.IsAbs(bin) {
		if abs, err := filepath.Abs(bin); err == nil {
			bin = abs
		}
	}
	work, err := os.MkdirTemp("", "maddog-skillopt-"+safeName(task.ID)+"-")
	if err != nil {
		return result, err
	}
	if opts.KeepWorkspace {
		result.Workspace = work
	} else {
		defer os.RemoveAll(work)
	}
	if seed := filepath.Join(task.Dir, "workdir"); isDir(seed) {
		if err := copyDir(seed, work); err != nil {
			return result, fmt.Errorf("copy task seed: %w", err)
		}
	}
	if configPath := strings.TrimSpace(opts.ProjectConfig); configPath != "" {
		if !isFile(configPath) {
			return result, fmt.Errorf("project config %q is not a file", configPath)
		}
		if err := copyFile(configPath, filepath.Join(work, "maddog.toml")); err != nil {
			return result, fmt.Errorf("copy project config: %w", err)
		}
	}
	if strings.TrimSpace(opts.Skill.Name) != "" {
		content := strings.TrimSpace(opts.SkillMarkdown)
		if content == "" {
			content = renderSkill(opts.Skill)
		}
		if err := installSkill(work, opts.Skill.Name, content); err != nil {
			return result, err
		}
	}

	timeout := time.Duration(task.TimeoutSec) * time.Second
	caseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	metricsPath := filepath.Join(work, ".skillopt-run-metrics.json")
	args := []string{"run", "--eval-mode", "--metrics", metricsPath}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if task.MaxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(task.MaxSteps))
	}
	if opts.Skill.Name != "" {
		args = append(args, "--skill", opts.Skill.Name)
	}
	args = append(args, task.Prompt)
	cmd := exec.CommandContext(caseCtx, bin, args...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), opts.Environment...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		result.RunError = err.Error()
	}
	_ = readJSON(metricsPath, &result.Metrics)
	result.Trace = boundedTrace(stdout.String(), stderr.String())

	hard, soft, gradeErr := Grade(work, task.Dir)
	result.Hard = hard
	result.Soft = soft
	result.Passed = result.RunError == "" && gradeErr == nil && hard >= 1
	if result.RunError != "" {
		// A seeded workspace can satisfy a verifier even when the target agent
		// never ran successfully. Preserve the verifier diagnostics, but never
		// award optimization credit to a failed rollout process.
		result.Soft = 0
	}
	if gradeErr != nil {
		result.GradeError = gradeErr.Error()
	}
	result.Duration = time.Since(started)
	return result, nil
}

// Grade runs the hidden verifier and returns normalized hard/soft scores.
func Grade(work, taskDir string) (float64, float64, error) {
	if err := clearVerifierReservedPaths(work); err != nil {
		return 0, 0, err
	}
	if verify := filepath.Join(taskDir, "verify.py"); isFile(verify) {
		dst := filepath.Join(work, ".skillopt-verify.py")
		if err := copyFileNew(verify, dst); err != nil {
			return 0, 0, err
		}
		if err := runCommand(work, "python", filepath.Base(dst)); err != nil {
			return 0, 0, err
		}
		return readVerifierScore(work)
	}
	verify := filepath.Join(taskDir, "verify.sh")
	if !isFile(verify) {
		return 0, 0, fmt.Errorf("missing verify.py or verify.sh")
	}
	dst := filepath.Join(work, ".skillopt-verify.sh")
	if err := copyFileNew(verify, dst); err != nil {
		return 0, 0, err
	}
	if bash := usableBashPath(); bash != "" {
		if err := runCommand(work, bash, filepath.Base(dst)); err != nil {
			return 0, 0, err
		}
		return readVerifierScore(work)
	}
	if err := gradeShellFallback(work, dst); err != nil {
		return 0, 0, err
	}
	return readVerifierScore(work)
}

func clearVerifierReservedPaths(work string) error {
	for _, name := range []string{".skillopt-verify.py", ".skillopt-verify.sh", "skillopt-score.json", ".skillopt-score.json"} {
		path := filepath.Join(work, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect reserved verifier path %s: %w", name, err)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("clear reserved verifier path %s: %w", name, err)
		}
	}
	return nil
}

func installSkill(root, name, content string) error {
	if !skill.IsValidName(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	path := filepath.Join(root, ".maddog", "skills", name, skill.SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600)
}

func renderSkill(sk skill.Skill) string {
	var b strings.Builder
	b.WriteString("---\nname: " + sk.Name + "\ndescription: " + strings.TrimSpace(sk.Description) + "\n")
	if sk.RunAs != "" {
		b.WriteString("runAs: " + string(sk.RunAs) + "\n")
	}
	if len(sk.AllowedTools) > 0 {
		b.WriteString("allowed-tools: " + strings.Join(sk.AllowedTools, ", ") + "\n")
	}
	b.WriteString("---\n\n" + strings.TrimSpace(sk.Body) + "\n")
	return b.String()
}

func readVerifierScore(work string) (float64, float64, error) {
	var score struct {
		Hard float64 `json:"hard"`
		Soft float64 `json:"soft"`
	}
	for _, name := range []string{"skillopt-score.json", ".skillopt-score.json"} {
		path := filepath.Join(work, name)
		if !isFile(path) {
			continue
		}
		if err := readJSON(path, &score); err != nil {
			return 0, 0, fmt.Errorf("read verifier score %s: %w", name, err)
		}
		if score.Soft == 0 && score.Hard == 1 {
			score.Soft = 1
		}
		return clamp(score.Hard), clamp(score.Soft), nil
	}
	return 1, 1, nil
}

func gradeShellFallback(work, verify string) error {
	body, err := os.ReadFile(verify)
	if err != nil {
		return err
	}
	script := string(body)
	if strings.Contains(script, "python3 check_ready.py") {
		return runCommand(work, "python", "check_ready.py")
	}
	if code, ok := extractPythonHeredoc(script); ok {
		return runCommand(work, "python", "-c", code)
	}
	file, lower, ok := extractTrFile(script)
	if !ok {
		return fmt.Errorf("bash unavailable and verifier is not a supported portable pattern")
	}
	want, ok := extractWant(script)
	if !ok {
		return fmt.Errorf("portable verifier has no expected value")
	}
	data, err := os.ReadFile(filepath.Join(work, file))
	if err != nil {
		return err
	}
	got := strings.NewReplacer("\r", "", "\n", "").Replace(string(data))
	if strings.Contains(script, "[:space:]") {
		got = strings.Join(strings.Fields(string(data)), "")
	}
	if lower {
		got = strings.ToLower(got)
	}
	if got != want {
		return fmt.Errorf("%s normalized to %q, want %q", file, got, want)
	}
	return nil
}

func extractTrFile(script string) (string, bool, bool) {
	re := regexp.MustCompile(`got=\$\(tr -d '[^']+' < ([^)| ]+)`)
	m := re.FindStringSubmatch(script)
	if len(m) != 2 {
		return "", false, false
	}
	return m[1], strings.Contains(script, "[:upper:]") && strings.Contains(script, "[:lower:]"), true
}

func extractWant(script string) (string, bool) {
	m := regexp.MustCompile(`(?m)^want="([^"]*)"$`).FindStringSubmatch(script)
	return firstGroup(m)
}

func firstGroup(match []string) (string, bool) {
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func extractPythonHeredoc(script string) (string, bool) {
	start := strings.Index(script, "python3 - <<'PY'")
	if start < 0 {
		start = strings.Index(script, "python3 - <<PY")
	}
	if start < 0 {
		return "", false
	}
	rest := script[start:]
	line := strings.IndexAny(rest, "\r\n")
	if line < 0 {
		return "", false
	}
	code := strings.TrimLeft(rest[line:], "\r\n")
	end := strings.LastIndex(code, "\nPY")
	if end < 0 {
		return "", false
	}
	return strings.TrimRight(code[:end], "\r\n"), true
}

func usableBashPath() string {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return ""
	}
	if runtime.GOOS == "windows" {
		wsl := filepath.Join(strings.TrimSpace(os.Getenv("WINDIR")), "System32", "bash.exe")
		if samePath(bash, wsl) {
			return ""
		}
	}
	return bash
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func runCommand(work, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = work
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(output.String()))
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}

func copyFileNew(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}
	complete = true
	return nil
}

func boundedTrace(stdout, stderr string) string {
	trace := strings.TrimSpace("STDOUT:\n" + stdout + "\n\nSTDERR:\n" + stderr)
	if len(trace) <= traceLimit {
		return trace
	}
	return trace[:traceLimit] + "\n[trace truncated]"
}

func safeName(value string) string {
	value = regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "case"
	}
	return value
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
