// e2ebench runs the committed e2e task suite against a real provider and emits a
// markdown + JSON report (accuracy, cache-hit rate, token use, cost) for a PR.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type task struct {
	ID         string
	Prompt     string `toml:"prompt"`
	MaxSteps   int    `toml:"max_steps"`
	TimeoutSec int    `toml:"timeout_sec"`
	Tags       []string
	Requires   []string
	Expect     taskExpect
	dir        string
}

type taskExpect struct {
	MinCompactions          int  `toml:"min_compactions" json:"min_compactions,omitempty"`
	MinReadinessChecks      int  `toml:"min_readiness_checks" json:"min_readiness_checks,omitempty"`
	MinReadinessBlocks      int  `toml:"min_readiness_blocks" json:"min_readiness_blocks,omitempty"`
	MinReadinessRecoveries  int  `toml:"min_readiness_recoveries" json:"min_readiness_recoveries,omitempty"`
	MinUpgrades             int  `toml:"min_upgrades" json:"min_upgrades,omitempty"`
	MinAdvisorEvents        int  `toml:"min_advisor_events" json:"min_advisor_events,omitempty"`
	MinSkillGeneratedEvents int  `toml:"min_skill_generated_events" json:"min_skill_generated_events,omitempty"`
	MinToolCalls            int  `toml:"min_tool_calls" json:"min_tool_calls,omitempty"`
	MinToolTruncations      int  `toml:"min_tool_truncations" json:"min_tool_truncations,omitempty"`
	MaxToolErrors           *int `toml:"max_tool_errors" json:"max_tool_errors,omitempty"`
}

type runMetrics struct {
	PromptTokens                  int     `json:"prompt_tokens"`
	CompletionTokens              int     `json:"completion_tokens"`
	CacheHitTokens                int     `json:"cache_hit_tokens"`
	CacheMissTokens               int     `json:"cache_miss_tokens"`
	Steps                         int     `json:"steps"`
	Cost                          float64 `json:"cost"`
	Currency                      string  `json:"currency"`
	Compactions                   int     `json:"compactions"`
	ReadinessChecks               int     `json:"readiness_checks"`
	ReadinessAllowed              int     `json:"readiness_allowed"`
	ReadinessBlocks               int     `json:"readiness_blocks"`
	ReadinessRecoveries           int     `json:"readiness_recoveries"`
	ReadinessErrors               int     `json:"readiness_errors"`
	ReadinessMissingProjectChecks int     `json:"readiness_missing_project_checks"`
	ReadinessIncompleteTodos      int     `json:"readiness_incomplete_todos"`
	ReadinessCommandMismatches    int     `json:"readiness_command_mismatches"`
	UpgradeEvents                 int     `json:"upgrade_events"`
	AdvisorEvents                 int     `json:"advisor_events"`
	SkillGeneratedEvents          int     `json:"skill_generated_events"`
	BudgetExceededEvents          int     `json:"budget_exceeded_events"`
	ToolCalls                     int     `json:"tool_calls"`
	ToolErrors                    int     `json:"tool_errors"`
	ToolTruncations               int     `json:"tool_truncations"`
}

type result struct {
	task
	runMetrics
	Passed  bool
	Skipped bool
	Note    string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "e2ebench — Maddog end-to-end benchmark.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", flag.CommandLine.Name())
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  # Run the committed suite:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s\n\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
		fmt.Fprintf(flag.CommandLine.Output(), "  # Grade a PR's diff with a retry budget:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %[1]s -mode diff -base origin/main -repo . -attempts 3 -timeout 1800\n", strings.Replace(flag.CommandLine.Name(), "e2ebench", "go run ./cmd/e2ebench", 1))
	}

	mode := flag.String("mode", "suite", "suite | manifest | diff (diff = generate tests for the PR diff and grade with the repo's tests)")
	suite := flag.String("suite", "benchmarks/e2e", "suite root (contains tasks/<id>/)")
	bin := flag.String("bin", "maddog", "path to the maddog binary")
	model := flag.String("model", "", "provider/model name (default: config default)")
	taskFilter := flag.String("tasks", "", "comma-separated task IDs to run or include in the manifest")
	tagFilter := flag.String("tags", "", "comma-separated tags to run or include in the manifest")
	outMD := flag.String("out", "", "write the markdown report here (default: stdout)")
	outJSON := flag.String("json", "", "write the JSON report here (optional)")
	budget := flag.Int("budget", 400_000, "abort once total tokens cross this (0 = no cap)")
	// diff-mode flags
	repo := flag.String("repo", ".", "repo root (diff mode)")
	base := flag.String("base", "", "base ref to diff the PR head against (diff mode)")
	testCmd := flag.String("test-cmd", "go test", "grader command run on the affected packages (diff mode)")
	maxSteps := flag.Int("max-steps", 80, "agent tool-call cap for the diff task")
	timeoutSec := flag.Int("timeout", 1200, "agent timeout in seconds (diff mode)")
	attempts := flag.Int("attempts", 1, "diff mode: retry up to N times until a run passes (stochastic agent)")
	flag.Parse()

	if *mode == "diff" {
		report := runDiff(diffOpts{
			bin: *bin, model: *model, repo: *repo, base: *base,
			testCmd: *testCmd, maxSteps: *maxSteps, timeoutSec: *timeoutSec, attempts: *attempts,
		})
		emit(report, *outMD, "")
		return
	}

	tasks, err := loadTasks(*suite)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load suite:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		dir := filepath.Join(*suite, "tasks")
		if _, statErr := os.Stat(dir); statErr != nil {
			fmt.Fprintf(os.Stderr, "no tasks found under %s: %v\n", dir, statErr)
		} else {
			fmt.Fprintf(os.Stderr, "no tasks found under %s (the directory exists but contains no task.toml files)\n", dir)
		}
		os.Exit(1)
	}
	tasks, err = filterTasks(tasks, *taskFilter, *tagFilter)
	if err != nil {
		fmt.Fprintln(os.Stderr, "filter suite:", err)
		os.Exit(1)
	}
	if *mode == "manifest" {
		report := buildManifest(tasks)
		rendered := renderManifest(report)
		emit(rendered, *outMD, "")
		if *outJSON != "" {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				fmt.Fprintln(os.Stderr, "marshal manifest json:", err)
				os.Exit(1)
			}
			if err := os.WriteFile(*outJSON, b, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "write manifest json:", err)
				os.Exit(1)
			}
		}
		if !report.Valid {
			os.Exit(1)
		}
		return
	}

	var results []result
	total := 0
	for _, t := range tasks {
		if *budget > 0 && total >= *budget {
			results = append(results, result{task: t, Skipped: true, Note: "skipped: token budget reached"})
			continue
		}
		r := runTask(*bin, *model, t)
		applyExpectations(&r)
		total += r.PromptTokens + r.CompletionTokens
		results = append(results, r)
	}

	report := render(results)
	if *outMD != "" {
		if err := os.WriteFile(*outMD, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(report)
	}
	if *outJSON != "" {
		b, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal json:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outJSON, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
			os.Exit(1)
		}
	}
}

func emit(report, outMD, _ string) {
	if outMD != "" {
		if err := os.WriteFile(outMD, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Print(report)
}

func loadTasks(suite string) ([]task, error) {
	tasksDir := filepath.Join(suite, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, err
	}
	var tasks []task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, e.Name())
		var t task
		if _, err := toml.DecodeFile(filepath.Join(dir, "task.toml"), &t); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		t.ID = e.Name()
		t.dir = dir
		if t.TimeoutSec == 0 {
			t.TimeoutSec = 240
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

func filterTasks(tasks []task, ids, tags string) ([]task, error) {
	wantIDs := csvSet(ids)
	wantTags := csvSet(tags)
	if len(wantIDs) == 0 && len(wantTags) == 0 {
		return tasks, nil
	}
	var out []task
	seen := map[string]bool{}
	for _, t := range tasks {
		if matchesTaskFilter(t, wantIDs, wantTags) {
			out = append(out, t)
			seen[t.ID] = true
		}
	}
	for id := range wantIDs {
		if !seen[id] && !taskIDExists(tasks, id) {
			return nil, fmt.Errorf("unknown task %q", id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tasks matched -tasks=%q -tags=%q", ids, tags)
	}
	return out, nil
}

func matchesTaskFilter(t task, wantIDs, wantTags map[string]bool) bool {
	if wantIDs[strings.ToLower(t.ID)] {
		return true
	}
	if len(wantTags) == 0 {
		return false
	}
	for _, tag := range t.Tags {
		if wantTags[strings.ToLower(strings.TrimSpace(tag))] {
			return true
		}
	}
	return false
}

func taskIDExists(tasks []task, id string) bool {
	for _, t := range tasks {
		if strings.EqualFold(t.ID, id) {
			return true
		}
	}
	return false
}

func csvSet(value string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out[part] = true
		}
	}
	return out
}

// runTask copies the task's seed workdir into a temp dir, runs the agent there,
// then drops in verify.sh and runs it as the grader. The grader is added only
// after the run so the agent can't read the answer key.
func runTask(bin, model string, t task) result {
	r := result{task: t}
	bin = resolveBinPath(bin)

	work, err := os.MkdirTemp("", "e2ebench-"+t.ID+"-")
	if err != nil {
		r.Note = "mktemp: " + err.Error()
		return r
	}
	defer os.RemoveAll(work)

	if seed := filepath.Join(t.dir, "workdir"); dirExists(seed) {
		if err := copyDir(seed, work); err != nil {
			r.Note = "copy seed: " + err.Error()
			return r
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.TimeoutSec)*time.Second)
	defer cancel()

	metricsPath := filepath.Join(work, ".run-metrics.json")
	args := []string{"run", "--metrics", metricsPath}
	if model != "" {
		args = append(args, "--model", model)
	}
	if t.MaxSteps > 0 {
		args = append(args, "--max-steps", fmt.Sprint(t.MaxSteps))
	}
	args = append(args, t.Prompt)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr // stream the run to the job log, keep stdout clean for the report
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 10 * time.Second // bound the wait for a stuck child after ctx timeout
	runErr := cmd.Run()

	if m, err := readMetrics(metricsPath); err == nil {
		r.runMetrics = m
	}
	if runErr != nil {
		r.Note = "run: " + runErr.Error()
		// still grade — a non-zero exit may just be a max-steps notice
	}

	passed, gradeErr := grade(work, t.dir)
	r.Passed = passed
	if gradeErr != "" {
		if strings.TrimSpace(r.Note) != "" {
			r.Note += "; "
		}
		r.Note += "grade: " + gradeErr
	}
	return r
}

func resolveBinPath(bin string) string {
	if bin == "" || filepath.IsAbs(bin) || !strings.ContainsAny(bin, `/\`) {
		return bin
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		return bin
	}
	return abs
}

func grade(work, taskDir string) (bool, string) {
	verify := filepath.Join(taskDir, "verify.sh")
	if !fileExists(verify) {
		return false, "missing verify.sh"
	}
	dst := filepath.Join(work, "verify.sh")
	if err := copyFile(verify, dst); err != nil {
		return false, "copy verify.sh: " + err.Error()
	}
	if _, err := exec.LookPath("bash"); err != nil {
		return gradeWithoutBash(work, dst)
	}
	env := os.Environ()
	if shimDir, cleanup, err := python3ShimDir(); err == nil && shimDir != "" {
		defer cleanup()
		env = append(env, "PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cmd := exec.Command("bash", "verify.sh")
	cmd.Dir = work
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func gradeWithoutBash(work, verify string) (bool, string) {
	body, err := os.ReadFile(verify)
	if err != nil {
		return false, "read verify.sh: " + err.Error()
	}
	script := string(body)
	if strings.Contains(script, "python3 check_ready.py") {
		return runGradeCommand(work, "python", "check_ready.py")
	}
	if code, ok := extractPythonHeredoc(script); ok {
		return runPythonCode(work, code)
	}
	if ok, msg, handled := gradeKnownShellVerifier(work, script); handled {
		return ok, msg
	}
	return false, "bash not found and verify.sh is not a supported python verifier"
}

func gradeKnownShellVerifier(work, script string) (bool, string, bool) {
	if strings.Contains(script, "test -f result.txt") {
		if !fileExists(filepath.Join(work, "result.txt")) {
			return false, "result.txt does not exist", true
		}
		got, err := normalizedFile(filepath.Join(work, "result.txt"), normalizeWhitespace)
		if err != nil {
			return false, err.Error(), true
		}
		if got != "86" {
			return false, fmt.Sprintf("result.txt normalized to %q, want 86", got), true
		}
		return true, "", true
	}

	file, lower, ok := extractTrFile(script)
	if !ok {
		return false, "", false
	}
	want, ok := extractWant(script)
	if !ok {
		return false, "", false
	}
	normalizer := normalizeNoCRLF
	if strings.Contains(script, "[:space:]") {
		normalizer = normalizeWhitespace
	}
	got, err := normalizedFile(filepath.Join(work, file), normalizer)
	if err != nil {
		return false, err.Error(), true
	}
	if lower {
		got = strings.ToLower(got)
	}
	if got != want {
		return false, fmt.Sprintf("%s normalized to %q, want %q", file, got, want), true
	}
	return true, "", true
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
	re := regexp.MustCompile(`(?m)^want="([^"]*)"$`)
	m := re.FindStringSubmatch(script)
	if len(m) != 2 {
		return "", false
	}
	return m[1], true
}

func normalizedFile(path string, normalize func(string) string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return normalize(string(b)), nil
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func normalizeNoCRLF(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
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
	firstNewline := strings.IndexAny(rest, "\r\n")
	if firstNewline < 0 {
		return "", false
	}
	code := rest[firstNewline:]
	code = strings.TrimLeft(code, "\r\n")
	end := strings.LastIndex(code, "\nPY")
	if end < 0 {
		if strings.TrimSpace(code) == "PY" {
			return "", true
		}
		return "", false
	}
	return strings.TrimRight(code[:end], "\r\n"), true
}

func runPythonCode(work, code string) (bool, string) {
	cmd := exec.Command("python", "-c", code)
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func runGradeCommand(work string, name string, args ...string) (bool, string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = work
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func python3ShimDir() (string, func(), error) {
	if _, err := exec.LookPath("python"); err != nil {
		return "", func() {}, nil
	}
	dir, err := os.MkdirTemp("", "e2ebench-python3-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	shim := "#!/bin/sh\nexec python \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "python3"), []byte(shim), 0o755); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if os.PathSeparator == '\\' {
		cmdShim := "@echo off\r\npython %*\r\n"
		if err := os.WriteFile(filepath.Join(dir, "python3.cmd"), []byte(cmdShim), 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dir, cleanup, nil
}

func render(results []result) string {
	var b strings.Builder
	passed, ran := 0, 0
	var pTok, cTok, hit, miss, compacts int
	var cost float64
	currency := ""
	for _, r := range results {
		if r.Skipped {
			continue
		}
		ran++
		if r.Passed {
			passed++
		}
		pTok += r.PromptTokens
		cTok += r.CompletionTokens
		hit += r.CacheHitTokens
		miss += r.CacheMissTokens
		compacts += r.Compactions
		cost += r.Cost
		if r.Currency != "" {
			currency = r.Currency
		}
	}

	fmt.Fprintf(&b, "## 🤖 Maddog e2e benchmark\n\n")
	fmt.Fprintf(&b, "**Accuracy:** %d/%d (%s) · **Cache hit:** %s · **Tokens:** %s (prompt %s / completion %s) · **Compactions:** %d · **Cost:** %s%.4f\n\n",
		passed, ran, pct(passed, ran), pct(hit, hit+miss),
		comma(pTok+cTok), comma(pTok), comma(cTok), compacts, currencySym(currency), cost)

	fmt.Fprintf(&b, "| Task | Result | Tags | Steps | Prompt | Completion | Cache hit | Compact | Mechanisms | Readiness | Cost |\n")
	fmt.Fprintf(&b, "|------|--------|------|------:|-------:|-----------:|----------:|--------:|------------|-----------|-----:|\n")
	for _, r := range results {
		tags := tagCell(r.Tags)
		switch {
		case r.Skipped:
			fmt.Fprintf(&b, "| `%s` | ⏭️ skipped | %s | — | — | — | — | — | — | — | — |\n", r.ID, tags)
		default:
			res := "❌ fail"
			if r.Passed {
				res = "✅ pass"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %d | %s | %s | %s | %d | %s | %s | %s%.4f |\n",
				r.ID, res, tags, r.Steps, comma(r.PromptTokens), comma(r.CompletionTokens),
				pct(r.CacheHitTokens, r.CacheHitTokens+r.CacheMissTokens),
				r.Compactions, mechanismCell(r.runMetrics), readinessCell(r.runMetrics), currencySym(r.Currency), r.Cost)
		}
	}
	fmt.Fprintf(&b, "\n<sub>Real provider run. Cache-hit %% is cached prompt tokens / total prompt tokens.</sub>\n")

	notes := false
	for _, r := range results {
		if r.Note != "" {
			if !notes {
				fmt.Fprintf(&b, "\n<details><summary>Notes</summary>\n\n")
				notes = true
			}
			fmt.Fprintf(&b, "- `%s`: %s\n", r.ID, r.Note)
		}
	}
	if notes {
		fmt.Fprintf(&b, "\n</details>\n")
	}
	return b.String()
}

type manifestReport struct {
	Valid       bool                `json:"valid"`
	Tasks       []manifestTask      `json:"tasks"`
	Tags        map[string]int      `json:"tags"`
	Requires    map[string]int      `json:"requires"`
	Requirement map[string][]string `json:"requirement_coverage"`
	Issues      []string            `json:"issues"`
}

type manifestTask struct {
	ID        string     `json:"id"`
	Tags      []string   `json:"tags"`
	Requires  []string   `json:"requires"`
	Expect    taskExpect `json:"expect,omitempty"`
	MaxSteps  int        `json:"max_steps"`
	Timeout   int        `json:"timeout_sec"`
	HasSeed   bool       `json:"has_seed_workdir"`
	HasVerify bool       `json:"has_verify"`
	Issues    []string   `json:"issues,omitempty"`
}

func buildManifest(tasks []task) manifestReport {
	report := manifestReport{
		Valid:       true,
		Tags:        map[string]int{},
		Requires:    map[string]int{},
		Requirement: map[string][]string{},
	}
	for _, t := range tasks {
		mt := manifestTask{
			ID:        t.ID,
			Tags:      append([]string(nil), t.Tags...),
			Requires:  append([]string(nil), t.Requires...),
			Expect:    t.Expect,
			MaxSteps:  t.MaxSteps,
			Timeout:   t.TimeoutSec,
			HasSeed:   dirExists(filepath.Join(t.dir, "workdir")),
			HasVerify: fileExists(filepath.Join(t.dir, "verify.sh")),
		}
		if strings.TrimSpace(t.Prompt) == "" {
			mt.Issues = append(mt.Issues, "missing prompt")
		}
		if len(t.Tags) == 0 {
			mt.Issues = append(mt.Issues, "missing tags")
		}
		if !mt.HasVerify {
			mt.Issues = append(mt.Issues, "missing verify.sh")
		}
		for _, tag := range t.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			report.Tags[tag]++
		}
		for _, req := range t.Requires {
			req = strings.TrimSpace(req)
			if req == "" {
				continue
			}
			report.Requires[req]++
			report.Requirement[req] = append(report.Requirement[req], t.ID)
		}
		if len(mt.Issues) > 0 {
			report.Valid = false
			for _, issue := range mt.Issues {
				report.Issues = append(report.Issues, fmt.Sprintf("%s: %s", t.ID, issue))
			}
		}
		report.Tasks = append(report.Tasks, mt)
	}
	return report
}

func renderManifest(report manifestReport) string {
	var b strings.Builder
	status := "valid"
	if !report.Valid {
		status = "invalid"
	}
	fmt.Fprintf(&b, "## Maddog e2e benchmark manifest\n\n")
	fmt.Fprintf(&b, "**Status:** %s · **Tasks:** %d · **Tags:** %d · **Requirements:** %d\n\n",
		status, len(report.Tasks), len(report.Tags), len(report.Requires))
	fmt.Fprintf(&b, "| Task | Tags | Requires | Expectations | Max steps | Timeout | Seed | Verify | Issues |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---:|---:|---|---|---|\n")
	for _, t := range report.Tasks {
		issues := "—"
		if len(t.Issues) > 0 {
			issues = strings.Join(t.Issues, "; ")
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %d | %d | %s | %s | %s |\n",
			t.ID, tagCell(t.Tags), tagCell(t.Requires), expectationCell(t.Expect), t.MaxSteps, t.Timeout,
			yesNo(t.HasSeed), yesNo(t.HasVerify), issues)
	}
	if len(report.Tags) > 0 {
		fmt.Fprintf(&b, "\n### Tag Coverage\n\n")
		for _, tag := range sortedKeys(report.Tags) {
			fmt.Fprintf(&b, "- `%s`: %d task(s)\n", tag, report.Tags[tag])
		}
	}
	if len(report.Requires) > 0 {
		fmt.Fprintf(&b, "\n### Requirement Coverage\n\n")
		for _, req := range sortedKeys(report.Requires) {
			fmt.Fprintf(&b, "- `%s`: `%s`\n", req, strings.Join(report.Requirement[req], "`, `"))
		}
	}
	if len(report.Issues) > 0 {
		fmt.Fprintf(&b, "\n### Issues\n\n")
		for _, issue := range report.Issues {
			fmt.Fprintf(&b, "- %s\n", issue)
		}
	}
	return b.String()
}

func applyExpectations(r *result) {
	if r == nil || !r.Passed {
		return
	}
	var misses []string
	expectAtLeast := func(label string, got, want int) {
		if want > 0 && got < want {
			misses = append(misses, fmt.Sprintf("%s %d < %d", label, got, want))
		}
	}
	e := r.Expect
	expectAtLeast("compactions", r.Compactions, e.MinCompactions)
	expectAtLeast("readiness checks", r.ReadinessChecks, e.MinReadinessChecks)
	expectAtLeast("readiness blocks", r.ReadinessBlocks, e.MinReadinessBlocks)
	expectAtLeast("readiness recoveries", r.ReadinessRecoveries, e.MinReadinessRecoveries)
	expectAtLeast("upgrades", r.UpgradeEvents, e.MinUpgrades)
	expectAtLeast("advisor events", r.AdvisorEvents, e.MinAdvisorEvents)
	expectAtLeast("skill generated events", r.SkillGeneratedEvents, e.MinSkillGeneratedEvents)
	expectAtLeast("tool calls", r.ToolCalls, e.MinToolCalls)
	expectAtLeast("tool truncations", r.ToolTruncations, e.MinToolTruncations)
	if e.MaxToolErrors != nil && r.ToolErrors > *e.MaxToolErrors {
		misses = append(misses, fmt.Sprintf("tool errors %d > %d", r.ToolErrors, *e.MaxToolErrors))
	}
	if len(misses) == 0 {
		return
	}
	r.Passed = false
	note := "expectation failure: " + strings.Join(misses, "; ")
	if strings.TrimSpace(r.Note) != "" {
		r.Note += "; " + note
	} else {
		r.Note = note
	}
}

func tagCell(tags []string) string {
	if len(tags) == 0 {
		return "—"
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			out = append(out, "`"+tag+"`")
		}
	}
	if len(out) == 0 {
		return "—"
	}
	return strings.Join(out, ", ")
}

func expectationCell(e taskExpect) string {
	var parts []string
	addMin := func(label string, n int) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s >= %d", label, n))
		}
	}
	addMin("compactions", e.MinCompactions)
	addMin("readiness checks", e.MinReadinessChecks)
	addMin("readiness blocks", e.MinReadinessBlocks)
	addMin("readiness recoveries", e.MinReadinessRecoveries)
	addMin("upgrades", e.MinUpgrades)
	addMin("advisor", e.MinAdvisorEvents)
	addMin("dynamic skills", e.MinSkillGeneratedEvents)
	addMin("tool calls", e.MinToolCalls)
	addMin("tool truncations", e.MinToolTruncations)
	if e.MaxToolErrors != nil {
		parts = append(parts, fmt.Sprintf("tool errors <= %d", *e.MaxToolErrors))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, "; ")
}

func readinessCell(m runMetrics) string {
	if m.ReadinessChecks == 0 {
		return "—"
	}
	parts := []string{fmt.Sprintf("%d checks", m.ReadinessChecks)}
	if m.ReadinessBlocks > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", m.ReadinessBlocks))
	}
	if m.ReadinessRecoveries > 0 {
		parts = append(parts, fmt.Sprintf("%d recovered", m.ReadinessRecoveries))
	}
	if m.ReadinessErrors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", m.ReadinessErrors))
	}
	if m.ReadinessMissingProjectChecks > 0 {
		parts = append(parts, fmt.Sprintf("missing checks %d", m.ReadinessMissingProjectChecks))
	}
	if m.ReadinessIncompleteTodos > 0 {
		parts = append(parts, fmt.Sprintf("incomplete todos %d", m.ReadinessIncompleteTodos))
	}
	if m.ReadinessCommandMismatches > 0 {
		parts = append(parts, fmt.Sprintf("command mismatches %d", m.ReadinessCommandMismatches))
	}
	return strings.Join(parts, "; ")
}

func mechanismCell(m runMetrics) string {
	var parts []string
	if m.UpgradeEvents > 0 {
		parts = append(parts, fmt.Sprintf("upgrades %d", m.UpgradeEvents))
	}
	if m.AdvisorEvents > 0 {
		parts = append(parts, fmt.Sprintf("advisor %d", m.AdvisorEvents))
	}
	if m.SkillGeneratedEvents > 0 {
		parts = append(parts, fmt.Sprintf("dynamic skills %d", m.SkillGeneratedEvents))
	}
	if m.BudgetExceededEvents > 0 {
		parts = append(parts, fmt.Sprintf("budget %d", m.BudgetExceededEvents))
	}
	if m.ToolCalls > 0 {
		tool := fmt.Sprintf("tools %d", m.ToolCalls)
		if m.ToolErrors > 0 {
			tool += fmt.Sprintf(" err %d", m.ToolErrors)
		}
		if m.ToolTruncations > 0 {
			tool += fmt.Sprintf(" trunc %d", m.ToolTruncations)
		}
		parts = append(parts, tool)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, "; ")
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func pct(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

func comma(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func currencySym(c string) string {
	if c == "" {
		return ""
	}
	return c + " "
}

func readMetrics(path string) (runMetrics, error) {
	var m runMetrics
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip symlinks so a seed link can't leak a file from outside the seed tree.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
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
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	// Mirror the source mode so a seed's read-only / exec bit survives the copy.
	return os.Chmod(dst, info.Mode().Perm())
}
