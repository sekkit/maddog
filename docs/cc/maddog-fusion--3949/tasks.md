---
slugid: maddog-fusion--3949
stage: tasks
date: 2026-06-08
plan: docs/cc/maddog-fusion--3949/plan.md
---

# Tasks

## Unit A1: Provider acceptance tests
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/provider/openai/openai_test.go`, `Maddog/internal/provider/anthropic/anthropic_test.go`, `Maddog/internal/config/*_test.go`, `Maddog/internal/boot/*_test.go`
- **Depends on**: 无
- **Result**: Provider packages pass with local stream fixtures forced direct: `go test ./internal/provider ./internal/provider/openai ./internal/provider/anthropic ./internal/provider/costwrap -count=1`.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds test fixture stabilization.

## Unit B1: Evidence FailureSignal
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/evidence/evidence.go`, `Maddog/internal/evidence/evidence_test.go`
- **Depends on**: 无
- **Result**: `maddog/internal/evidence` passed in focused and broad internal test runs.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit B2: UpgradePolicy and agent routing loop
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/agent/upgrade.go`, `Maddog/internal/agent/upgrade_test.go`, `Maddog/internal/agent/agent.go`, `Maddog/internal/agent/testutil/mock_provider.go`
- **Depends on**: Unit B1
- **Result**: `maddog/internal/agent` full package passed after local SSE fixtures were isolated from machine proxy settings.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds test fixture stabilization.

## Unit B3: AgentConfig and boot wiring
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/config/config.go`, `Maddog/internal/config/default_test.go`, `Maddog/internal/boot/boot.go`, `Maddog/internal/boot/boot_test.go`, `Maddog/maddog.example.toml`
- **Depends on**: Unit B2, Unit B4, Unit B5
- **Result**: `maddog/internal/config` and `maddog/internal/boot` full package tests pass; boot local provider tests now run direct against `httptest`.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds test fixture stabilization.

## Unit B4: Frontier provider cost wrapper
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/provider/costwrap/costwrap.go`, `Maddog/internal/provider/costwrap/costwrap_test.go`, `Maddog/internal/agent/agent.go`
- **Depends on**: 无
- **Result**: `maddog/internal/provider/costwrap` passed standalone and in provider matrix; `maddog/internal/agent` budget routing tests passed in full package.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit B5: Built-in advisor skill
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/skill/builtin_advisor.go`, `Maddog/internal/skill/builtins.go`, `Maddog/internal/skill/skill_test.go`, `Maddog/internal/boot/subagent_model_test.go`
- **Depends on**: 无
- **Result**: `maddog/internal/skill` and `maddog/internal/boot` full package tests pass, including advisor model precedence.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit B6: Runtime event kinds and UI wire-up
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/event/event.go`, `Maddog/internal/event/event_test.go`, `Maddog/internal/serve/wire.go`, `Maddog/internal/serve/wire_test.go`, `Maddog/desktop/wire.go`, `Maddog/desktop/wire_test.go`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/frontend/src/components/*`, `Maddog/desktop/frontend/src/styles.css`
- **Depends on**: 无
- **Result**: `maddog/internal/event`, `maddog/internal/serve`, focused desktop wire tests, frontend `npm run typecheck`, and `npm run check:css` pass.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-A: Runtime skill injection store
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/skill/skill.go`, `Maddog/internal/skill/skill_test.go`
- **Depends on**: 无
- **Result**: `maddog/internal/skill` full package tests pass, including injected skill read/list/remove coverage.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-B: Dynamic skill validator
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/skill/validator.go`, `Maddog/internal/skill/skill_test.go`
- **Depends on**: 无
- **Result**: `maddog/internal/skill` full package tests pass, including validator accept/reject paths and high-risk task rejection.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-C: Skill matcher and dynamic generator
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/skill/matcher.go`, `Maddog/internal/skill/generator.go`, `Maddog/internal/skill/orchestrator.go`, `Maddog/internal/skill/skill_test.go`
- **Depends on**: Unit C1-A, Unit C1-B
- **Result**: `maddog/internal/skill` full package tests pass, including generator retry, existing-skill matching, dynamic generation, and high-risk skip behavior.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-D: Controller orchestration integration
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/control/controller.go`, `Maddog/internal/control/input.go`, `Maddog/internal/control/input_test.go`, `Maddog/internal/control/controller_test.go`, `Maddog/internal/boot/boot.go`
- **Depends on**: Unit C1-C
- **Result**: `maddog/internal/control` and `maddog/internal/boot` full package tests pass, including runtime orchestration hint integration.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-A: Replay bundle capture
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/eval/replay.go`, `Maddog/internal/eval/eval_test.go`, `Maddog/internal/control/controller.go`, `Maddog/internal/control/controller_test.go`, `Maddog/internal/cli/run_metrics.go`, `Maddog/internal/cli/run_metrics_test.go`
- **Depends on**: Unit C1-D
- **Result**: `maddog/internal/eval`, `maddog/internal/control`, and focused CLI eval/run-metrics tests pass.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-B: Replay runner
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/eval/runner.go`, `Maddog/internal/eval/eval_test.go`
- **Depends on**: Unit C2-A
- **Result**: `maddog/internal/eval` full package tests pass, including replay runner coverage.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-C: Frontier scorer
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/eval/scorer.go`, `Maddog/internal/eval/eval_test.go`
- **Depends on**: Unit C2-B
- **Result**: `maddog/internal/eval` full package tests pass, including scorer parsing/fallback coverage.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-D: Guardrail and skill promotion
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/eval/guardrail.go`, `Maddog/internal/eval/promote.go`, `Maddog/internal/eval/eval_test.go`, `Maddog/internal/cli/eval_cli.go`, `Maddog/internal/cli/eval_cli_test.go`
- **Depends on**: Unit C2-C
- **Result**: `maddog/internal/eval` full package tests and focused `maddog/internal/cli` eval guard/promote tests pass.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit E2E: Cross-stage runtime verification
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/agent/upgrade_test.go`, `Maddog/internal/control/input_test.go`, `Maddog/internal/control/controller_test.go`, `Maddog/internal/cli/eval_cli_test.go`, `Maddog/desktop/frontend/src/lib/bridge.ts`, `Maddog/desktop/frontend/src/App.tsx`
- **Depends on**: Unit B3, Unit B6, Unit C1-D, Unit C2-D
- **Result**: Cross-stage focused Go tests, frontend typecheck, CSS checks, and previously captured real CLI/runtime-preview E2E validate the plan path end to end.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds validation fixture stabilization and persistent task state.

## Unit B7: Automatic advisor consultation budget and context curation
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/agent/upgrade.go`, `Maddog/internal/agent/advisor.go`, `Maddog/internal/agent/agent.go`, `Maddog/internal/agent/upgrade_test.go`, `Maddog/internal/config/config.go`, `Maddog/internal/config/default_test.go`, `Maddog/internal/boot/boot.go`, `Maddog/internal/skill/builtin_advisor.go`
- **Depends on**: Unit B2, Unit B5, Unit B6
- **Started-at-commit**: a5844225
- **Result**: Automatic upgrade decisions now trigger a Go-native advisor consultation before frontier routing, with per-turn/session budgets, curated failure context, structured `event.Advisor`, and frontier-visible guidance. Verified by `go test ./internal/agent ./internal/config ./internal/provider/anthropic ./internal/boot ./internal/serve ./internal/cli -count=1` and full `go test ./... -count=1`.
- **Commit**: pending

## Unit B8: Anthropic native advisor tool support
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/provider/provider.go`, `Maddog/internal/provider/anthropic/anthropic.go`, `Maddog/internal/provider/anthropic/anthropic_test.go`
- **Depends on**: Unit B7
- **Result**: Added opt-in provider-native advisor config, Anthropic beta header/tool schema support, native server-block preservation/replay, and request exposure tests. Native advisor remains disabled by default. Verified by `go test ./internal/provider ./internal/provider/anthropic -count=1` within full `go test ./... -count=1`.
- **Commit**: pending

## Unit B9: Desktop advisor event presentation
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/event/event.go`, `Maddog/internal/event/event_test.go`, `Maddog/internal/serve/wire.go`, `Maddog/internal/serve/wire_test.go`, `Maddog/desktop/wire.go`, `Maddog/desktop/wire_test.go`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/frontend/src/components/Message.tsx`, `Maddog/desktop/frontend/src/components/Transcript.tsx`, `Maddog/desktop/frontend/src/styles.css`
- **Depends on**: Unit B7
- **Result**: Advisor events now serialize through serve and Wails wire contracts, render as a dedicated desktop transcript card with reason/question/advice/budget metadata, export to Markdown, and remain visible in CLI/TUI sinks. Verified by `go test ./internal/serve -count=1`, `go test . -count=1` in `Maddog/desktop`, plus `npm run typecheck`, `npm run check:css`, and `npm run build`.
- **Commit**: pending

## Post-v1 Unit D1: Provider profile and role projection
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/desktop/settings_app.go`, `Maddog/desktop/settings_app_test.go`, `Maddog/desktop/frontend/src/lib/types.ts`
- **Depends on**: none
- **Started-at-commit**: 616c19179dc7c192afd3fc2a69fa160f9182980c
- **Result**: Desktop Settings now projects provider profile metadata from existing provider entries and model refs: derived roles (`default`, `planner`, `frontier`, `small`), gateway classification, normalized auth mode, credential env/status, frontier budget/eligibility, small-model eligibility, and dangling provider/model warnings. Token values are not serialized. Verified by `go test . -run "TestSettingsProviderProfiles" -count=1` and `go test . -count=1` in `Maddog/desktop`, plus frontend `npm run test:all` and `npm run build`.
- **Review**: Included in code-quality reviewer pass after one metadata follow-up on the adjacent E1 wire path.
- **Commit**: pending

## Post-v1 Unit E1: Tool output compressor and raw-result lookup
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/contextpack/compressor.go`, `Maddog/internal/contextpack/compressor_test.go`, `Maddog/internal/agent/agent.go`, `Maddog/internal/agent/contextpack_test.go`, `Maddog/internal/control/controller.go`, `Maddog/internal/control/tool_result_test.go`, `Maddog/internal/event/event.go`, `Maddog/internal/serve/wire.go`, `Maddog/internal/serve/wire_test.go`, `Maddog/desktop/wire.go`, `Maddog/desktop/wire_test.go`, `Maddog/desktop/frontend/src/lib/types.ts`
- **Depends on**: none
- **Started-at-commit**: 616c19179dc7c192afd3fc2a69fa160f9182980c
- **Result**: Added deterministic `contextpack` compression with failure-signal preservation, log dedupe, head/tail fallback, UTF-8 safe trimming, Windows path-line preservation, raw refs, and char/token delta estimates. Agent tool results now feed compressed model-visible content while retaining full raw output for `Controller.ToolResult`; compression metadata is emitted through event, serve wire, desktop wire, and TypeScript contracts. Panic/error fallback uses raw truncated output and warns. Verified by `go test ./internal/contextpack -count=1`, `go test ./internal/agent -count=1`, `go test ./internal/control -count=1`, `go test ./internal/serve -count=1`, `go test . -count=1` in `Maddog/desktop`, plus frontend `npm run test:all` and `npm run build`.
- **Review**: Spec reviewer passed. Code-quality reviewer found one P2 metrics issue; fixed with a failing-then-passing test asserting `ToolResult` compression metrics match final visible output, then reviewer passed.
- **Commit**: pending

## Post-v1 Unit D2: Official auth, API key, and icodeeasy probe parity
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/provider/openai/fetch_models.go`, `Maddog/internal/provider/openai/fetch_models_test.go`, `Maddog/internal/provider/openai/openai.go`, `Maddog/internal/config/fetch.go`, `Maddog/internal/config/fetch_test.go`, `Maddog/desktop/settings_app.go`, `Maddog/desktop/settings_app_test.go`, `Maddog/desktop/frontend/src/components/SettingsPanel.tsx`, `Maddog/desktop/frontend/src/__tests__/maddog-mechanisms-contract.test.ts`
- **Depends on**: Post-v1 Unit D1
- **Started-at-commit**: 3e253982de85c707d65d74b42c21d64d32312460
- **Result**: Settings/model probe now reuses runtime provider auth semantics for OpenAI-compatible/API-key, official bearer, workload identity, and icodeeasy/custom base URLs. `ProviderEntry.FetchModels` passes `AuthConfig` into OpenAI-compatible fetch; bearer and WIF probes use the same headers/token exchange as runtime requests; fetch/probe 401/403 returns `provider.AuthError` with provider/env/status metadata without token leakage. Desktop `FetchProviderModels` carries full WIF metadata, and Settings provider cards display the active credential env with backend-compatible fallback. Verified by `go test ./internal/provider/openai ./internal/config -count=1`, `go test . -count=1` in `Maddog/desktop`, frontend `npm run test:all`, and `npm run build`.
- **Review**: Spec reviewer initially found missing WIF fields in desktop probe and untyped token-exchange auth errors; both were fixed with failing-then-passing tests and spec re-review passed. Code-quality reviewer found a bearer credential-env fallback mismatch in frontend display/gating; fixed with a failing-then-passing contract test and quality re-review passed.
- **Commit**: pending

## Post-v1 Unit D3: Provider usage, budget, and status event
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/provider/costwrap/costwrap.go`, `Maddog/internal/provider/retry.go`, `Maddog/internal/provider/openai/responses.go`, `Maddog/internal/provider/anthropic/anthropic.go`, `Maddog/internal/agent/agent.go`, `Maddog/internal/agent/coordinator.go`, `Maddog/internal/agent/task.go`, `Maddog/internal/event/event.go`, `Maddog/internal/serve/wire.go`, `Maddog/desktop/wire.go`, `Maddog/desktop/app.go`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/frontend/src/components/StatusBar.tsx`, `Maddog/internal/provider/costwrap/costwrap_test.go`, `Maddog/internal/provider/openai/openai_test.go`, `Maddog/internal/provider/anthropic/anthropic_test.go`, `Maddog/internal/agent/upgrade_test.go`, `Maddog/internal/agent/usage_profile_test.go`, `Maddog/internal/agent/coordinator_test.go`, `Maddog/internal/agent/task_test.go`, `Maddog/internal/boot/boot_test.go`, `Maddog/internal/serve/wire_test.go`, `Maddog/desktop/wire_test.go`, `Maddog/desktop/frontend/src/__tests__/use-controller-meta.test.ts`, `Maddog/desktop/frontend/src/__tests__/status-bar.test.ts`
- **Depends on**: Post-v1 Unit D1
- **Started-at-commit**: dab2eadcf698bca802d1712160c7516b99c8ee73
- **Result**: Runtime usage events now carry provider role/model/effort, frontier budget snapshots, and provider health/auth/rate/balance status for default, planner, frontier, task subagent, and skill subagent paths. Provider request errors emit standalone `provider_status` events, while cancellation/deadline exits do not create false provider health failures. Anthropic and OpenAI Responses structured stream errors are preserved as typed `provider.APIError` values so auth, rate-limit, balance, and degraded statuses classify reliably. Serve and desktop wire contracts round-trip `usage.profile`, `usage.providerStatus`, and top-level `providerStatus`; the GUI stores latest provider status per tab and StatusBar exposes opt-in provider, frontier budget, provider health, and rate-limit items without changing defaults. Verified by `go test ./internal/provider/openai ./internal/provider/anthropic ./internal/provider/costwrap ./internal/agent ./internal/serve ./internal/control ./internal/config ./internal/boot -count=1`, `go test . -count=1` in `Maddog/desktop`, frontend `npm run test:all`, `npm run check:css`, `npm run build`, and `git diff --check`.
- **Review**: Spec reviewer initially required planner and skill-subagent coverage; added coordinator and boot integration tests, then passed. Code-quality reviewer found stale StatusBar status precedence, cancellation/deadline false provider errors, structured stream auth/rate error classification gaps, and missing nested subagent status forwarding; all were fixed with failing-then-passing tests and final quality review passed.
- **Commit**: included in `feat(provider): add runtime status telemetry`

## Post-v1 Unit E2: Shell/test/log compression and context metrics
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/contextpack/compressor.go`, `Maddog/internal/contextpack/shell.go`, `Maddog/internal/contextpack/shell_test.go`, `Maddog/internal/control/controller.go`, `Maddog/internal/event/event.go`, `Maddog/internal/serve/wire.go`, `Maddog/desktop/frontend/src/components/ToolCard.tsx`, `Maddog/desktop/frontend/src/components/ContextPanel.tsx`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/internal/serve/wire_test.go`, `Maddog/desktop/frontend/src/__tests__/context-panel-breakdown.test.ts`
- **Depends on**: Post-v1 Unit E1
- **Started-at-commit**: 163adce532eb9a7cf7739c5059344504928ff6b2
- **Result**: Added deterministic shell/test/log compression strategies for Go test, npm test, npm build, ripgrep, git status, git diff, and repeated server logs. Summaries preserve failure names, file:line paths, expected/actual/error details, representative matches, repeated-line counts, and tail lines under tight context budgets. Desktop tab telemetry now tracks latest-turn compression raw/compressed/saved char and token metrics, restores compression-only snapshots, and exposes the breakdown in ContextPanel; ToolCard keeps a compression badge while archived full output remains loadable through raw result lookup. Verified by `go test ./internal/contextpack ./internal/agent ./internal/control ./internal/serve -count=1`, `go test . -count=1` in `Maddog/desktop`, frontend `npm run test:all`, `npm run check:css`, `npm run build`, and `git diff --check`.
- **Review**: Spec reviewer initially required npm test, git diff/status, tail preservation, per-turn breakdown, and full-output lookup coverage; all were fixed with failing-then-passing tests and spec re-review passed. Code-quality reviewer found tight-budget header priority, greedy rg parsing, and compression-only telemetry restore gaps; all were fixed with tests and final quality re-review passed.
- **Commit**: pending

## Post-v1 Unit E3: Context policy, raw-data externalization, and disable switch
- **Status**: completed
- **Execution note**: test-first
- **Files**: `Maddog/internal/contextpack/compressor.go`, `Maddog/internal/contextpack/compressor_test.go`, `Maddog/internal/config/config.go`, `Maddog/internal/config/edit.go`, `Maddog/internal/config/render.go`, `Maddog/internal/config/default_test.go`, `Maddog/internal/config/edit_test.go`, `Maddog/internal/boot/boot.go`, `Maddog/internal/agent/agent.go`, `Maddog/internal/agent/contextpack_test.go`, `Maddog/internal/control/controller.go`, `Maddog/internal/control/tool_result_test.go`, `Maddog/internal/cli/run_metrics.go`, `Maddog/internal/cli/run_metrics_test.go`, `Maddog/desktop/sessions.go`, `Maddog/desktop/sessions_test.go`, `Maddog/desktop/settings_app.go`, `Maddog/desktop/settings_app_test.go`, `Maddog/desktop/frontend/package.json`, `Maddog/desktop/frontend/src/components/SettingsPanel.tsx`, `Maddog/desktop/frontend/src/components/ToolCard.tsx`, `Maddog/desktop/frontend/src/lib/bridge.ts`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/frontend/src/lib/useController.ts`, `Maddog/desktop/frontend/src/locales/en.ts`, `Maddog/desktop/frontend/src/locales/zh.ts`, `Maddog/desktop/frontend/src/__tests__/context-compression-settings.test.ts`, `Maddog/desktop/frontend/src/__tests__/tool-raw-unavailable.test.ts`
- **Depends on**: Post-v1 Unit E1, Post-v1 Unit E2
- **Started-at-commit**: b7d7b763
- **Result**: Added context compression policy controls (`off`, `auto`, `aggressive`) across config defaults, editing, TOML rendering, boot options, desktop Settings, and frontend bridge/types/locales. Raw tool results now persist to a session-scoped `raw-tool-results/<branchID>` store, rebind across initial session paths, `SetSessionPath`, `Resume`, new/clear session flows, and move through desktop trash/restore/purge lifecycle. Missing raw sidecars return compressed fallback plus `rawUnavailable`, which ToolCard renders as a localized note; raw store write failures clear in-memory raw, warn, and keep compressed-only output. CLI run metrics now aggregate compression counts and char/token savings. Verified by `go test ./internal/contextpack ./internal/config ./internal/agent ./internal/control ./internal/cli ./internal/boot -count=1`, `go test . -count=1` in `Maddog/desktop`, frontend `npm run test:all`, frontend `npm run build`, and `git diff --check`.
- **Review**: Spec reviewer initially required `Controller.Resume` raw-store rebinding, desktop raw sidecar trash/restore/purge lifecycle, and ToolCard raw-unavailable display; all were fixed with failing-then-passing tests and spec re-review passed. Code-quality reviewer found the same raw-unavailable frontend gap; fixed with SSR behavior coverage and quality re-review passed.
- **Commit**: baa752f3

## Post-v1 Unit F1: Code intelligence backend registry
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/internal/codegraph/backend.go`, `Maddog/internal/codegraph/backend_test.go`, `Maddog/internal/codegraph/codegraph.go`, `Maddog/internal/config/config.go`, `Maddog/internal/control/codegraph_mcp_test.go`, `Maddog/desktop/app.go`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/capabilities_app_test.go`
- **Depends on**: none

## Post-v1 Unit F2: Code intelligence benchmark harness
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/internal/codegraph/bench.go`, `Maddog/internal/codegraph/bench_test.go`, `Maddog/cmd/codeintelbench/main.go`, `Maddog/internal/doctor/report.go`, `Maddog/internal/doctor/report_test.go`
- **Depends on**: Post-v1 Unit F1

## Post-v1 Unit F3: MCP code backend GUI management
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/desktop/app.go`, `Maddog/desktop/frontend/src/components/CapabilitiesPanel.tsx`, `Maddog/desktop/frontend/src/lib/bridge.ts`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/frontend/src/locales/en.ts`, `Maddog/desktop/frontend/src/locales/zh.ts`, `Maddog/desktop/capabilities_app_test.go`, `Maddog/desktop/frontend/src/__tests__/capabilities-panel.test.ts`
- **Depends on**: Post-v1 Unit F1, Post-v1 Unit F2

## Post-v1 Unit G1: Replay eval bundle v2 and SkillOpt-style candidate lifecycle
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/internal/skilleval/bundle.go`, `Maddog/internal/skilleval/bundle_test.go`, `Maddog/internal/skilleval/candidate.go`, `Maddog/internal/skilleval/candidate_test.go`, `Maddog/internal/control/controller.go`, `Maddog/internal/skill/skill.go`, `Maddog/internal/event/event.go`, `Maddog/internal/control/input_test.go`
- **Depends on**: Post-v1 Unit E1

## Post-v1 Unit G2: Replay runner, guardrail, and promotion scoring
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/internal/skilleval/runner.go`, `Maddog/internal/skilleval/runner_test.go`, `Maddog/internal/skilleval/scorer.go`, `Maddog/internal/skilleval/scorer_test.go`, `Maddog/internal/skilleval/guardrail.go`, `Maddog/internal/skilleval/guardrail_test.go`, `Maddog/internal/cli/skilleval.go`, `Maddog/internal/cli/skilleval_test.go`
- **Depends on**: Post-v1 Unit G1

## Post-v1 Unit G3: Skill management GUI and promotion audit
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/desktop/app.go`, `Maddog/desktop/frontend/src/components/CapabilitiesPanel.tsx`, `Maddog/desktop/frontend/src/components/MemoryPanel.tsx`, `Maddog/desktop/frontend/src/lib/bridge.ts`, `Maddog/desktop/frontend/src/lib/types.ts`, `Maddog/desktop/frontend/src/locales/en.ts`, `Maddog/desktop/frontend/src/locales/zh.ts`, `Maddog/desktop/frontend/src/styles.css`, `Maddog/desktop/skills_app_test.go`, `Maddog/desktop/frontend/src/__tests__/skills-panel.test.ts`, `Maddog/desktop/frontend/src/__tests__/memory-suggestions.test.ts`
- **Depends on**: Post-v1 Unit G1, Post-v1 Unit G2

## Post-v1 Unit G4: Rule/LLM hybrid code review skill
- **Status**: pending
- **Execution note**: test-first
- **Files**: `Maddog/internal/review/rules.go`, `Maddog/internal/review/rules_test.go`, `Maddog/internal/review/report.go`, `Maddog/internal/review/report_test.go`, `Maddog/internal/skill/builtins.go`, `Maddog/internal/skill/skill_test.go`
- **Depends on**: Post-v1 Unit F1, Post-v1 Unit G1
