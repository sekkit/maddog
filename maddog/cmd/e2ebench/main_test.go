package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTasksIncludesBenchmarkMetadata(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks", "metadata-task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(`
prompt = "write the answer"
max_steps = 7
timeout_sec = 42
tags = ["provider", "skill", "desktop-parity"]
requires = ["api-key", "filesystem"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadTasks(root)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	got := tasks[0]
	if strings.Join(got.Tags, ",") != "provider,skill,desktop-parity" {
		t.Fatalf("tags = %#v", got.Tags)
	}
	if strings.Join(got.Requires, ",") != "api-key,filesystem" {
		t.Fatalf("requires = %#v", got.Requires)
	}
}

func TestManifestValidationRequiresRunnableTaskShape(t *testing.T) {
	root := t.TempDir()
	complete := filepath.Join(root, "tasks", "complete")
	missingVerify := filepath.Join(root, "tasks", "missing-verify")
	missingTag := filepath.Join(root, "tasks", "missing-tag")
	for _, dir := range []string{complete, missingVerify, missingTag} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(complete, "task.toml"), []byte(`
prompt = "ok"
tags = ["core"]
requires = ["filesystem"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(complete, "verify.sh"), []byte("exit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missingVerify, "task.toml"), []byte(`
prompt = "ok"
tags = ["provider"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missingTag, "task.toml"), []byte(`
prompt = "ok"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadTasks(root)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	report := buildManifest(tasks)
	if report.Valid {
		t.Fatal("manifest should be invalid when tasks miss verify scripts or tags")
	}
	if len(report.Tasks) != 3 {
		t.Fatalf("manifest tasks = %d, want 3", len(report.Tasks))
	}
	text := renderManifest(report)
	for _, want := range []string{
		"## Maddog e2e benchmark manifest",
		"`complete`",
		"`missing-verify`",
		"missing verify.sh",
		"missing tags",
		"`provider`",
		"`core`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manifest missing %q:\n%s", want, text)
		}
	}
}

func TestFilterTasksByIDAndTags(t *testing.T) {
	tasks := []task{
		{ID: "compaction", Tags: []string{"context", "tinyctx"}},
		{ID: "provider-auth", Tags: []string{"provider", "auth", "frontier"}},
		{ID: "skill", Tags: []string{"skill", "dynamic"}},
	}

	got, err := filterTasks(tasks, "provider-auth", "dynamic", "")
	if err != nil {
		t.Fatalf("filterTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("filtered tasks = %d, want 2 (%#v)", len(got), got)
	}
	if got[0].ID != "provider-auth" || got[1].ID != "skill" {
		t.Fatalf("filtered order = %s, %s", got[0].ID, got[1].ID)
	}

	got, err = filterTasks(tasks, "", "frontier,tinyctx", "")
	if err != nil {
		t.Fatalf("filterTasks tags only: %v", err)
	}
	if len(got) != 2 || got[0].ID != "compaction" || got[1].ID != "provider-auth" {
		t.Fatalf("tag filtered tasks = %#v", got)
	}
}

func TestFilterTasksExcludesTagsAfterIncludes(t *testing.T) {
	tasks := []task{
		{ID: "local-provider-tool-loop", Tags: []string{"local-fixture", "provider"}},
		{ID: "provider-auth", Tags: []string{"provider", "auth", "frontier"}},
		{ID: "local-frontier-upgrade", Tags: []string{"local-fixture", "frontier"}},
		{ID: "compaction", Tags: []string{"tinyctx"}},
	}

	got, err := filterTasks(tasks, "", "", "local-fixture")
	if err != nil {
		t.Fatalf("filterTasks exclude only: %v", err)
	}
	if len(got) != 2 || got[0].ID != "provider-auth" || got[1].ID != "compaction" {
		t.Fatalf("exclude filtered tasks = %#v", got)
	}

	got, err = filterTasks(tasks, "", "provider,frontier", "local-fixture")
	if err != nil {
		t.Fatalf("filterTasks include and exclude: %v", err)
	}
	if len(got) != 1 || got[0].ID != "provider-auth" {
		t.Fatalf("include/exclude filtered tasks = %#v", got)
	}
}

func TestFilterTasksRejectsUnknownTag(t *testing.T) {
	_, err := filterTasks([]task{{ID: "one", Tags: []string{"core"}}}, "", "desktop", "")
	if err == nil {
		t.Fatal("filterTasks should reject filters that match no tasks")
	}
	if !strings.Contains(err.Error(), "no tasks matched") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterTasksRejectsUnknownExcludeTag(t *testing.T) {
	_, err := filterTasks([]task{{ID: "one", Tags: []string{"core"}}}, "", "", "desktop")
	if err == nil {
		t.Fatal("filterTasks should reject exclude filters that match no known tags")
	}
	if !strings.Contains(err.Error(), `unknown exclude tag "desktop"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterTasksRejectsUnknownID(t *testing.T) {
	_, err := filterTasks([]task{{ID: "one", Tags: []string{"core"}}}, "missing", "", "")
	if err == nil {
		t.Fatal("filterTasks should reject unknown task IDs")
	}
	if !strings.Contains(err.Error(), `unknown task "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalProviderFixtureRunUsesDedicatedTaskAndModel(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks", "local-provider-tool-loop")
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(`
prompt = "fixture"
max_steps = 4
timeout_sec = 30
tags = ["local-fixture", "provider", "tool-loop", "metrics", "headless-cli"]
requires = ["local-openai-fixture", "filesystem"]

[expect]
min_tool_calls = 1
max_tool_errors = 0
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("set -e\ntest -f fixture-output.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadTasks(root)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	suite, err := newLocalFixtureSuite(tasks, "maddog-bin")
	if err != nil {
		t.Fatalf("newLocalFixtureSuite: %v", err)
	}
	defer suite.Close()

	if suite.task.ID != "local-provider-tool-loop" {
		t.Fatalf("fixture task = %q", suite.task.ID)
	}
	if suite.model != "local-openai-fixture/local-tool-model" {
		t.Fatalf("fixture model = %q", suite.model)
	}
	cfg, err := os.ReadFile(filepath.Join(suite.task.dir, "workdir", "maddog.toml"))
	if err != nil {
		t.Fatalf("fixture config missing: %v", err)
	}
	for _, want := range []string{
		`default_model = "local-openai-fixture/local-tool-model"`,
		`name = "local-openai-fixture"`,
		`kind = "openai"`,
		`model = "local-tool-model"`,
		`api_key_env = "MADDOG_LOCAL_FIXTURE_KEY"`,
		suite.server.URL,
	} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("fixture config missing %q:\n%s", want, cfg)
		}
	}
}

func TestLocalProviderFixtureRunCanTargetAnthropicNativeTask(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks", "local-anthropic-tool-loop")
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(`
prompt = "fixture"
max_steps = 4
timeout_sec = 30
tags = ["local-fixture", "anthropic", "frontier", "provider", "tool-loop", "metrics", "headless-cli"]
requires = ["local-anthropic-fixture", "filesystem"]

[expect]
min_tool_calls = 1
max_tool_errors = 0
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("set -e\ntest -f anthropic-fixture-output.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadTasks(root)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	suite, err := newLocalFixtureSuite(tasks, "maddog-bin", localFixtureKindAnthropic)
	if err != nil {
		t.Fatalf("newLocalFixtureSuite: %v", err)
	}
	defer suite.Close()

	if suite.task.ID != "local-anthropic-tool-loop" {
		t.Fatalf("fixture task = %q", suite.task.ID)
	}
	if suite.model != "local-anthropic-fixture/claude-local-frontier" {
		t.Fatalf("fixture model = %q", suite.model)
	}
	cfg, err := os.ReadFile(filepath.Join(suite.task.dir, "workdir", "maddog.toml"))
	if err != nil {
		t.Fatalf("fixture config missing: %v", err)
	}
	for _, want := range []string{
		`default_model = "local-anthropic-fixture/claude-local-frontier"`,
		`name = "local-anthropic-fixture"`,
		`kind = "anthropic"`,
		`model = "claude-local-frontier"`,
		`api_key_env = "MADDOG_LOCAL_ANTHROPIC_FIXTURE_KEY"`,
		suite.server.URL,
	} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("fixture config missing %q:\n%s", want, cfg)
		}
	}
}

func TestLocalProviderFixtureRunCanTargetFrontierUpgradeTask(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks", "local-frontier-upgrade")
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(`
prompt = "fixture"
max_steps = 6
timeout_sec = 30
tags = ["local-fixture", "frontier", "upgrade", "small-model", "provider", "metrics", "headless-cli"]
requires = ["local-frontier-fixture", "filesystem"]

[expect]
min_upgrades = 1
min_tool_calls = 3
max_tool_errors = 3
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("set -e\npython3 - <<'PY'\nimport json\nfrom pathlib import Path\nmetrics=json.loads(Path('.run-metrics.json').read_text())\nassert metrics['upgrade_events'] >= 1\nPY\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadTasks(root)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	suite, err := newLocalFixtureSuite(tasks, "maddog-bin", localFixtureKindFrontierUpgrade)
	if err != nil {
		t.Fatalf("newLocalFixtureSuite: %v", err)
	}
	defer suite.Close()

	if suite.task.ID != "local-frontier-upgrade" {
		t.Fatalf("fixture task = %q", suite.task.ID)
	}
	if suite.model != "local-small-fixture/local-small-model" {
		t.Fatalf("fixture model = %q", suite.model)
	}
	cfg, err := os.ReadFile(filepath.Join(suite.task.dir, "workdir", "maddog.toml"))
	if err != nil {
		t.Fatalf("fixture config missing: %v", err)
	}
	for _, want := range []string{
		`default_model = "local-small-fixture/local-small-model"`,
		`frontier_model = "local-frontier-fixture/local-frontier-model"`,
		`upgrade_enabled = true`,
		`upgrade_threshold = 3`,
		`name = "local-small-fixture"`,
		`name = "local-frontier-fixture"`,
		suite.server.URL,
	} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("fixture config missing %q:\n%s", want, cfg)
		}
	}
}

func TestLocalProviderFixtureRunCanTargetOfficialAuthTask(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks", "local-official-auth")
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.toml"), []byte(`
prompt = "fixture"
max_steps = 1
timeout_sec = 30
tags = ["local-fixture", "official-auth", "auth", "openai", "anthropic", "provider", "metrics", "headless-cli"]
requires = ["local-official-auth-fixture", "filesystem"]

[expect]
min_tool_calls = 0
max_tool_errors = 0
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("set -e\ntest -f auth-fixture-observations.json\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadTasks(root)
	if err != nil {
		t.Fatalf("loadTasks: %v", err)
	}
	suite, err := newLocalFixtureSuite(tasks, "maddog-bin", localFixtureKindOfficialAuth)
	if err != nil {
		t.Fatalf("newLocalFixtureSuite: %v", err)
	}
	defer suite.Close()

	if suite.task.ID != "local-official-auth" {
		t.Fatalf("fixture task = %q", suite.task.ID)
	}
	if suite.model != "local-openai-official/local-openai-official-model" {
		t.Fatalf("fixture model = %q", suite.model)
	}
	cfg, err := os.ReadFile(filepath.Join(suite.task.dir, "workdir", "maddog.toml"))
	if err != nil {
		t.Fatalf("fixture config missing: %v", err)
	}
	for _, want := range []string{
		`default_model = "local-openai-official/local-openai-official-model"`,
		`identity_env = "MADDOG_LOCAL_OPENAI_IDENTITY_TOKEN"`,
		`identity_provider_id = "wip_local"`,
		`subject_token_type = "urn:ietf:params:oauth:token-type:jwt"`,
		`service_account_id = "svc_openai_local"`,
		`auth_type = "workload_identity"`,
		`identity_env = "MADDOG_LOCAL_ANTHROPIC_IDENTITY_TOKEN"`,
		`federation_rule_id = "fdrl_local"`,
		`organization_id = "org_local"`,
		`service_account_id = "svac_local"`,
		suite.server.URL,
	} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("fixture config missing %q:\n%s", want, cfg)
		}
	}
}

func TestResolveBinPathKeepsPATHLookupAndAbsolutizesRelativePath(t *testing.T) {
	got := resolveBinPath("maddog")
	if got != "maddog" {
		t.Fatalf("PATH lookup binary should stay unchanged, got %q", got)
	}

	rel := filepath.Join(".", "bin", "maddog.exe")
	got = resolveBinPath(rel)
	if !filepath.IsAbs(got) {
		t.Fatalf("relative binary path should become absolute, got %q", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "/bin/maddog.exe") {
		t.Fatalf("absolute path has unexpected suffix: %q", got)
	}
}

func TestGradeUsesPython3ShimWhenOnlyPythonIsUsable(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	work := t.TempDir()
	taskDir := t.TempDir()
	verify := "set -e\npython3 - <<'PY'\nprint('ok')\nPY\n"
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte(verify), 0o755); err != nil {
		t.Fatal(err)
	}
	ok, msg := grade(work, taskDir)
	if !ok {
		t.Fatalf("grade should run verify.sh through the python3 shim: %s", msg)
	}
}

func TestGradeWithoutBashRunsPythonHeredoc(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	work := t.TempDir()
	verify := filepath.Join(work, "verify.sh")
	body := "set -e\npython3 - <<'PY'\nfrom pathlib import Path\nPath('ok.txt').write_text('ok', encoding='utf-8')\nPY\n"
	if err := os.WriteFile(verify, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	ok, msg := gradeWithoutBash(work, verify)
	if !ok {
		t.Fatalf("gradeWithoutBash failed: %s", msg)
	}
	if got, err := os.ReadFile(filepath.Join(work, "ok.txt")); err != nil || string(got) != "ok" {
		t.Fatalf("python heredoc did not run, got %q err=%v", got, err)
	}
}

func TestGradeWithoutBashRunsKnownShellComparisons(t *testing.T) {
	work := t.TempDir()
	cases := []struct {
		name    string
		file    string
		content string
		verify  string
	}{
		{
			name:    "whitespace lower compare",
			file:    "answer.txt",
			content: " AlderMoor-\nVerrin ",
			verify:  "set -e\ngot=$(tr -d '[:space:]' < answer.txt | tr '[:upper:]' '[:lower:]')\nwant=\"aldermoor-verrin\"\n[ \"$got\" = \"$want\" ] || { echo \"bad\"; exit 1; }\n",
		},
		{
			name:    "newline compare",
			file:    "skill-result.txt",
			content: "MADDOG-BENCH:maddog|skill|invoked\r\n",
			verify:  "set -e\ngot=$(tr -d '\\r\\n' < skill-result.txt)\nwant=\"MADDOG-BENCH:maddog|skill|invoked\"\n[ \"$got\" = \"$want\" ] || { echo \"bad\"; exit 1; }\n",
		},
		{
			name:    "test file then compare",
			file:    "result.txt",
			content: " 8\n6 ",
			verify:  "set -e\ntest -f result.txt\ngot=$(tr -d '[:space:]' < result.txt)\nif [ \"$got\" != \"86\" ]; then\n  echo \"bad\"\n  exit 1\nfi\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(work, tc.file), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			verify := filepath.Join(work, "verify.sh")
			if err := os.WriteFile(verify, []byte(tc.verify), 0o755); err != nil {
				t.Fatal(err)
			}
			ok, msg := gradeWithoutBash(work, verify)
			if !ok {
				t.Fatalf("gradeWithoutBash failed: %s", msg)
			}
		})
	}
}

func TestRenderIncludesReadinessMetrics(t *testing.T) {
	out := render([]result{{
		task: task{ID: "readiness", Tags: []string{"readiness"}},
		runMetrics: runMetrics{
			PromptTokens:                  10,
			CompletionTokens:              5,
			ReadinessChecks:               3,
			ReadinessAllowed:              2,
			ReadinessBlocks:               1,
			ReadinessMissingProjectChecks: 4,
			ReadinessIncompleteTodos:      5,
			ReadinessCommandMismatches:    6,
		},
		Passed: true,
	}})
	for _, want := range []string{
		"| `readiness` | ✅ pass | `readiness` |",
		"Readiness",
		"3 checks",
		"1 blocked",
		"missing checks 4",
		"incomplete todos 5",
		"command mismatches 6",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

func TestMetricExpectationsCanFailOtherwisePassingTask(t *testing.T) {
	maxToolErrors := 0
	r := result{
		task: task{
			ID: "mechanism",
			Expect: taskExpect{
				MinUpgrades:      1,
				MinAdvisorEvents: 1,
				MaxToolErrors:    &maxToolErrors,
			},
		},
		runMetrics: runMetrics{
			AdvisorEvents: 2,
			ToolErrors:    1,
		},
		Passed: true,
	}

	applyExpectations(&r)

	if r.Passed {
		t.Fatal("expectations should fail a task whose metrics do not meet the declared gates")
	}
	for _, want := range []string{
		"upgrades 0 < 1",
		"tool errors 1 > 0",
	} {
		if !strings.Contains(r.Note, want) {
			t.Fatalf("note missing %q: %q", want, r.Note)
		}
	}
	if strings.Contains(r.Note, "advisor") {
		t.Fatalf("advisor expectation should pass and stay out of note: %q", r.Note)
	}
}
