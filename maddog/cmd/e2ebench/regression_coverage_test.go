package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaddogBenchmarkCoverageAudit(t *testing.T) {
	root := repoRoot(t)
	tasks, err := loadTasks(filepath.Join(root, "benchmarks", "e2e"))
	if err != nil {
		t.Fatalf("load e2e tasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no committed e2e tasks found")
	}
	report := buildManifest(tasks)
	if !report.Valid {
		t.Fatalf("e2e manifest is invalid: %v", report.Issues)
	}

	requiredTags := []string{
		"advisor",
		"auth",
		"compaction",
		"desktop-parity",
		"frontier",
		"headless-cli",
		"local-fixture",
		"maddog-isolation",
		"metrics",
		"provider",
		"readiness",
		"skill",
		"small-model",
		"subagent",
		"tinyctx",
		"tool-loop",
		"upgrade",
	}
	for _, tag := range requiredTags {
		if report.Tags[tag] == 0 {
			t.Fatalf("e2e manifest has no task tagged %q", tag)
		}
	}

	providerTask := taskByID(t, tasks, "provider-auth-frontier-profile")
	for _, tag := range []string{"provider", "auth", "frontier", "small-model", "advisor", "upgrade", "desktop-parity"} {
		if !hasString(providerTask.Tags, tag) {
			t.Fatalf("provider-auth-frontier-profile missing tag %q: %#v", tag, providerTask.Tags)
		}
	}
	providerPrompt := providerTask.Prompt
	for _, want := range []string{"official-openai", "official-anthropic", "token_env", "advisor", "upgrade"} {
		if !strings.Contains(providerPrompt, want) {
			t.Fatalf("provider-auth-frontier-profile prompt should require %q:\n%s", want, providerPrompt)
		}
	}
	providerConfig := readRepoFile(t, root, "benchmarks", "e2e", "tasks", "provider-auth-frontier-profile", "workdir", "maddog.sample.toml")
	for _, want := range []string{
		`api_key_env = "ICODEEASY_API_KEY"`,
		`kind = "openai"`,
		`kind = "anthropic"`,
		`auth_type = "workload_identity"`,
		`identity_env = "OPENAI_IDENTITY_TOKEN"`,
		`identity_provider_id = "wip_openai"`,
		`identity_env = "ANTHROPIC_IDENTITY_TOKEN"`,
		`advisor_max_uses_per_turn = 1`,
		`advisor_max_context_messages = 6`,
	} {
		if !strings.Contains(providerConfig, want) {
			t.Fatalf("provider auth fixture missing %q", want)
		}
	}
	providerVerify := readRepoFile(t, root, "benchmarks", "e2e", "tasks", "provider-auth-frontier-profile", "verify.sh")
	for _, want := range []string{
		`providers["small"]["token_env"] == "ICODEEASY_API_KEY"`,
		`providers["frontier"]["kind"] == "anthropic"`,
		`providers["official-openai"]["auth_type"] == "workload_identity"`,
		`providers["official-anthropic"]["auth_type"] == "workload_identity"`,
		`data["upgrade_enabled"] is True`,
		`data["advisor"]["max_uses_per_turn"] == 1`,
		`desktop_provider_access`,
	} {
		if !strings.Contains(providerVerify, want) {
			t.Fatalf("provider auth verifier missing %q", want)
		}
	}

	localFixtureTask := taskByID(t, tasks, "local-provider-tool-loop")
	for _, tag := range []string{"local-fixture", "provider", "tool-loop", "metrics", "headless-cli"} {
		if !hasString(localFixtureTask.Tags, tag) {
			t.Fatalf("local-provider-tool-loop missing tag %q: %#v", tag, localFixtureTask.Tags)
		}
	}
	if !hasString(localFixtureTask.Requires, "local-openai-fixture") {
		t.Fatalf("local-provider-tool-loop should require local-openai-fixture: %#v", localFixtureTask.Requires)
	}
	if localFixtureTask.Expect.MinToolCalls < 1 {
		t.Fatalf("local-provider-tool-loop should require at least one tool call: %+v", localFixtureTask.Expect)
	}
	if localFixtureTask.Expect.MaxToolErrors == nil || *localFixtureTask.Expect.MaxToolErrors != 0 {
		t.Fatalf("local-provider-tool-loop should reject tool errors: %+v", localFixtureTask.Expect)
	}
	localFixtureVerify := readRepoFile(t, root, "benchmarks", "e2e", "tasks", "local-provider-tool-loop", "verify.sh")
	for _, want := range []string{
		"fixture-output.txt",
		".run-metrics.json",
		`metrics["tool_calls"] >= 1`,
		`metrics["tool_errors"] == 0`,
		`metrics["steps"] >= 2`,
	} {
		if !strings.Contains(localFixtureVerify, want) {
			t.Fatalf("local provider fixture verifier missing %q", want)
		}
	}

	anthropicFixtureTask := taskByID(t, tasks, "local-anthropic-tool-loop")
	for _, tag := range []string{"local-fixture", "anthropic", "frontier", "provider", "tool-loop", "metrics", "headless-cli"} {
		if !hasString(anthropicFixtureTask.Tags, tag) {
			t.Fatalf("local-anthropic-tool-loop missing tag %q: %#v", tag, anthropicFixtureTask.Tags)
		}
	}
	if !hasString(anthropicFixtureTask.Requires, "local-anthropic-fixture") {
		t.Fatalf("local-anthropic-tool-loop should require local-anthropic-fixture: %#v", anthropicFixtureTask.Requires)
	}
	if anthropicFixtureTask.Expect.MinToolCalls < 1 {
		t.Fatalf("local-anthropic-tool-loop should require at least one tool call: %+v", anthropicFixtureTask.Expect)
	}
	if anthropicFixtureTask.Expect.MaxToolErrors == nil || *anthropicFixtureTask.Expect.MaxToolErrors != 0 {
		t.Fatalf("local-anthropic-tool-loop should reject tool errors: %+v", anthropicFixtureTask.Expect)
	}
	anthropicFixtureVerify := readRepoFile(t, root, "benchmarks", "e2e", "tasks", "local-anthropic-tool-loop", "verify.sh")
	for _, want := range []string{
		"anthropic-fixture-output.txt",
		".run-metrics.json",
		`metrics["tool_calls"] >= 1`,
		`metrics["tool_errors"] == 0`,
		`metrics["steps"] >= 2`,
	} {
		if !strings.Contains(anthropicFixtureVerify, want) {
			t.Fatalf("local Anthropic fixture verifier missing %q", want)
		}
	}

	frontierFixtureTask := taskByID(t, tasks, "local-frontier-upgrade")
	for _, tag := range []string{"local-fixture", "frontier", "upgrade", "small-model", "provider", "metrics", "headless-cli"} {
		if !hasString(frontierFixtureTask.Tags, tag) {
			t.Fatalf("local-frontier-upgrade missing tag %q: %#v", tag, frontierFixtureTask.Tags)
		}
	}
	if !hasString(frontierFixtureTask.Requires, "local-frontier-fixture") {
		t.Fatalf("local-frontier-upgrade should require local-frontier-fixture: %#v", frontierFixtureTask.Requires)
	}
	if frontierFixtureTask.Expect.MinUpgrades < 1 || frontierFixtureTask.Expect.MinToolErrors < 3 {
		t.Fatalf("local-frontier-upgrade should require upgrade and tool-error metrics: %+v", frontierFixtureTask.Expect)
	}
	if frontierFixtureTask.Expect.MaxToolErrors == nil || *frontierFixtureTask.Expect.MaxToolErrors != 3 {
		t.Fatalf("local-frontier-upgrade should pin expected tool errors: %+v", frontierFixtureTask.Expect)
	}
	frontierFixtureVerify := readRepoFile(t, root, "benchmarks", "e2e", "tasks", "local-frontier-upgrade", "verify.sh")
	for _, want := range []string{
		".run-metrics.json",
		`metrics["upgrade_events"] >= 1`,
		`metrics["tool_errors"] == 3`,
		`metrics["steps"] >= 4`,
	} {
		if !strings.Contains(frontierFixtureVerify, want) {
			t.Fatalf("local frontier fixture verifier missing %q", want)
		}
	}

	officialAuthTask := taskByID(t, tasks, "local-official-auth")
	for _, tag := range []string{"local-fixture", "official-auth", "auth", "openai", "anthropic", "frontier", "provider", "metrics", "headless-cli"} {
		if !hasString(officialAuthTask.Tags, tag) {
			t.Fatalf("local-official-auth missing tag %q: %#v", tag, officialAuthTask.Tags)
		}
	}
	if !hasString(officialAuthTask.Requires, "local-official-auth-fixture") {
		t.Fatalf("local-official-auth should require local-official-auth-fixture: %#v", officialAuthTask.Requires)
	}
	if officialAuthTask.Expect.MinUpgrades < 1 || officialAuthTask.Expect.MinToolErrors < 1 {
		t.Fatalf("local-official-auth should require upgrade and tool-error metrics: %+v", officialAuthTask.Expect)
	}
	officialAuthVerify := readRepoFile(t, root, "benchmarks", "e2e", "tasks", "local-official-auth", "verify.sh")
	for _, want := range []string{
		"auth-fixture-observations.json",
		`openai_exchange["subject_token"] == "openai-local-identity-jwt"`,
		`obs["openai_authorization"] == "Bearer openai-official-access-token"`,
		`exchange["assertion"] == "anthropic-local-identity-jwt"`,
		`obs["anthropic_authorization"] == "Bearer anthropic-minted-local-token"`,
		`metrics["upgrade_events"] >= 1`,
	} {
		if !strings.Contains(officialAuthVerify, want) {
			t.Fatalf("local official auth verifier missing %q", want)
		}
	}

	testEvidence := map[string][]string{
		"OpenAI API-key and official workload identity auth": {
			"internal/provider/openai/openai_test.go:oauth-access-token",
			"internal/provider/openai/openai_test.go:identity_provider_id",
			"internal/provider/openai/openai_test.go:ICODEEASY_API_KEY",
		},
		"Anthropic official workload identity auth": {
			"internal/provider/anthropic/anthropic_test.go:workload_identity",
			"internal/provider/anthropic/anthropic_test.go:/v1/oauth/token",
		},
		"Frontier/small-model routing and advisor escalation": {
			"internal/agent/upgrade_test.go:ThresholdUpgradePolicy",
			"internal/agent/upgrade_test.go:AdvisorRunner",
			"internal/config/edit_test.go:frontier_budget",
		},
		"Advisor metrics and desktop wire presentation": {
			"internal/cli/run_metrics_test.go:AdvisorEvents",
			"desktop/wire_test.go:advisor payload",
		},
		"Tinyctx/compaction metrics and desktop history": {
			"internal/agent/compact_test.go:maybeCompact",
			"internal/cli/compaction_test.go:CompactionCardLines",
			"desktop/history_test.go:compaction_done",
		},
		"C2 replay/scoring/guardrail/promotion": {
			"internal/skilleval/bundle_test.go:TestCaptureBundle",
			"internal/skilleval/guardrail_test.go:TestGuardrailPassesAndRejectsPromotionRisks",
			"internal/skilleval/promote.go:PromoteSkill",
		},
		"Desktop provider settings and Maddog isolation": {
			"desktop/app_test.go:frontier settings",
			"desktop/app_test.go:workload_identity",
			"desktop/wails.json:\"outputfilename\": \"maddog-dev\"",
			"desktop/settings_app_test.go:openai",
			"desktop/branding_test.go:maddog-dev.exe",
			"scripts/desktop-build.sh:BINNAME=\"maddog-dev\"",
			"scripts/desktop-build.sh:build/bin/${BINNAME}.app",
			"scripts/run-maddog-regression.ps1:IncludeDesktopBuildSmoke",
			"scripts/run-maddog-regression.ps1:desktop/build/bin/maddog-dev.exe",
			"desktop/release_branding_test.go:${APPNAME}-windows-${arch}-installer.exe",
		},
		"Desktop GUI model/provider/advisor controls": {
			"desktop/frontend/package.json:maddog-mechanisms-contract.test.ts",
			"desktop/frontend/package.json:provider-model-refresh.test.ts",
			"desktop/frontend/src/__tests__/maddog-mechanisms-contract.test.ts:app.SetFrontierRoute(model, enabled, threshold, budget)",
			"desktop/frontend/src/__tests__/maddog-mechanisms-contract.test.ts:const AUTH_TYPES: readonly string[] = [\"api_key\", \"bearer\", \"workload_identity\"]",
			"desktop/frontend/src/__tests__/maddog-mechanisms-contract.test.ts:placeholder=\"ANTHROPIC_IDENTITY_TOKEN\"",
			"desktop/frontend/src/__tests__/maddog-mechanisms-contract.test.ts:event === \"upgrade\"",
			"desktop/frontend/src/__tests__/provider-model-refresh.test.ts:background refresh preserves manually curated model list",
		},
		"External coding benchmark Maddog adapter": {
			"benchmarks/coding-agent-benchmark/maddog.config.yaml:MADDOG_MODEL",
			"benchmarks/coding-agent-benchmark/maddog.config.yaml:MADDOG_BENCHMARK_LOCAL_KEY",
			"benchmarks/coding-agent-benchmark/maddog.config.yaml:{workspace}/.maddog-run-metrics.json",
			"scripts/run-coding-agent-benchmark.ps1:.benchmark\\maddog-home",
			"scripts/run-coding-agent-benchmark.ps1:SmokeOnly",
			"scripts/run-coding-agent-benchmark.ps1:LocalSmoke",
			"scripts/run-coding-agent-benchmark.ps1:Assert-LocalSmokeResult",
			"scripts/run-coding-agent-benchmark.ps1:tasks_fully_passed",
			"scripts/run-coding-agent-benchmark.ps1:Benchmark smoke fixture completed.",
			"scripts/run-coding-agent-benchmark.ps1:write_file",
			"cmd/coding-agent-benchmark-fixture/main.go:Benchmark smoke fixture completed.",
			"cmd/coding-agent-benchmark-fixture/main.go:math_utils.py",
		},
		"Offline OpenAI-compatible fixture, tool loop, and metrics": {
			"cmd/e2ebench/main.go:suite | manifest | diff | local-fixture",
			"cmd/e2ebench/local_fixture.go:local-provider-tool-loop",
			"cmd/e2ebench/local_fixture.go:/chat/completions",
			"cmd/e2ebench/local_fixture.go:tool_calls",
			"scripts/run-maddog-regression.ps1:local-provider-e2e",
		},
		"Offline Anthropic frontier fixture, tool loop, and metrics": {
			"cmd/e2ebench/local_fixture.go:local-anthropic-tool-loop",
			"cmd/e2ebench/local_fixture.go:/v1/messages",
			"cmd/e2ebench/local_fixture.go:tool_use",
			"cmd/e2ebench/local_fixture.go:tool_result",
			"scripts/run-maddog-regression.ps1:anthropic-native-sse",
		},
		"Offline frontier upgrade routing metrics": {
			"cmd/e2ebench/local_fixture.go:local-frontier-upgrade",
			"cmd/e2ebench/local_fixture.go:local-small-fixture",
			"cmd/e2ebench/local_fixture.go:local-frontier-fixture",
			"benchmarks/e2e/tasks/local-frontier-upgrade/verify.sh:upgrade_events",
			"scripts/run-maddog-regression.ps1:frontier-upgrade",
		},
		"Offline official auth provider paths": {
			"cmd/e2ebench/local_fixture.go:local-official-auth",
			"cmd/e2ebench/local_fixture.go:openai-official-access-token",
			"cmd/e2ebench/local_fixture.go:/oauth/token",
			"cmd/e2ebench/local_fixture.go:/v1/oauth/token",
			"cmd/e2ebench/local_fixture.go:anthropic-minted-local-token",
			"internal/config/config.go:workload identity reads a pre-minted access token when present",
			"benchmarks/e2e/tasks/local-official-auth/verify.sh:auth-fixture-observations.json",
		},
		"Live official auth smoke gate": {
			"cmd/e2ebench/official_auth_smoke.go:official-auth-smoke",
			"cmd/e2ebench/official_auth_smoke.go:OPENAI_OFFICIAL_TOKEN",
			"cmd/e2ebench/official_auth_smoke.go:OPENAI_IDENTITY_TOKEN",
			"cmd/e2ebench/official_auth_smoke.go:OPENAI_IDENTITY_PROVIDER_ID",
			"cmd/e2ebench/official_auth_smoke.go:ANTHROPIC_IDENTITY_TOKEN",
			"cmd/e2ebench/official_auth_smoke.go:ANTHROPIC_FEDERATION_RULE_ID",
			"cmd/e2ebench/official_auth_smoke.go:workload_identity",
			"cmd/e2ebench/live_official_auth_test.go:TestRunOfficialAuthSmokeUsesWorkloadIdentityForOpenAIAndAnthropic",
		},
		"Live frontier provider, advisor, cost, and scorer smoke gate": {
			"cmd/e2ebench/frontier_smoke.go:frontier-smoke",
			"cmd/e2ebench/frontier_smoke.go:ICODEEASY_API_KEY",
			"cmd/e2ebench/frontier_smoke.go:ANTHROPIC_API_KEY",
			"cmd/e2ebench/frontier_smoke.go:costwrap.New",
			"cmd/e2ebench/frontier_smoke.go:skilleval.ScoreReplay",
			"cmd/e2ebench/frontier_smoke.go:NativeAdvisor",
			"cmd/e2ebench/frontier_smoke_test.go:TestRunFrontierSmokeCoversProviderScorerCostAndAdvisor",
		},
	}
	for capability, evidences := range testEvidence {
		for _, evidence := range evidences {
			file, needle, ok := strings.Cut(evidence, ":")
			if !ok {
				t.Fatalf("bad evidence fixture %q", evidence)
			}
			body := readRepoFile(t, root, filepath.FromSlash(file))
			if !strings.Contains(body, needle) {
				t.Fatalf("%s evidence missing %q in %s", capability, needle, file)
			}
		}
	}

	regressionScript := readRepoFile(t, root, "scripts", "run-maddog-regression.ps1")
	for _, want := range []string{
		`-Name "coverage-audit"`,
		`TestMaddogBenchmarkCoverageAudit`,
		"official auth",
		"desktop-parity",
		"icodeeasy",
		"anthropic",
		"openai",
		"tinyctx",
		"advisor",
		"internal/skilleval",
		"coding-agent-benchmark",
		"LocalSmoke",
		"live_readiness",
		"live_requirements",
		"completion_audit",
		"failed_required_steps",
		"RequireComplete",
		"RequireRealCapabilities",
		"skipped_real_gates",
		"blocked_missing_credentials",
		"real_provider_verified = [bool]$ProviderAPIKeyE2EVerified",
		"external_backend_verified",
		"Assert-RealCapabilityAudit",
		"$ExternalBenchmarkRealVerified",
		"external_backend_verified = [bool]$ExternalBenchmarkRealVerified",
		"$RequireRealCapabilities -and -not (Assert-RealCapabilityAudit",
		"AuditOnly",
		"IncludeOfficialAuthSmoke",
		"IncludeDesktopBuildSmoke",
		"desktop-build-smoke",
		"maddog-dev.exe",
		"official-auth-smoke",
		"frontier-smoke",
		"frontier-scorer-live",
		"anthropic-advisor-live",
		"exclude-tags",
		`"-exclude-tags", "local-fixture"`,
		"partial-live-pending",
		"verified-offline",
		"official_auth_e2e_ready",
		"provider_api_key_e2e_ready",
		"any_of",
		"all_of",
		"Provider e2e ready",
		"Frontier smoke ready",
		"OPENAI_OFFICIAL_TOKEN",
		"OPENAI_IDENTITY_TOKEN",
		"OPENAI_IDENTITY_PROVIDER_ID",
		"ANTHROPIC_IDENTITY_TOKEN",
		"DEEPSEEK_API_KEY",
		"local-provider-e2e",
		"local-fixture",
		"frontier-upgrade",
		"official-auth",
	} {
		if !strings.Contains(strings.ToLower(regressionScript), strings.ToLower(want)) {
			t.Fatalf("run-maddog-regression.ps1 should mention coverage %q", want)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) && dirExists(filepath.Join(dir, "benchmarks", "e2e")) {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = next
	}
}

func taskByID(t *testing.T, tasks []task, id string) task {
	t.Helper()
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q not found", id)
	return task{}
}

func readRepoFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(body)
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
