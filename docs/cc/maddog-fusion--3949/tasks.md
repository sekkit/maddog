---
slugid: maddog-fusion--3949
stage: tasks
date: 2026-06-28
plan: docs/cc/maddog-fusion--3949/plan.md
active_plan: docs/cc/maddog-fusion--3949/plan-external-schemes.md
---

# Tasks

> Status note: the A/B/C units below are the completed historical fusion baseline from `plan.md`.
> The active development backlog for the loop-governed four-scheme plan starts at **Current Active Backlog** and follows `plan-external-schemes.md`.

## Unit A1: Provider acceptance tests
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/provider/openai/openai_test.go`, `DeepSeek-Reasonix/internal/provider/anthropic/anthropic_test.go`, `DeepSeek-Reasonix/internal/config/*_test.go`, `DeepSeek-Reasonix/internal/boot/*_test.go`
- **Depends on**: 无
- **Result**: Provider packages pass with local stream fixtures forced direct: `go test ./internal/provider ./internal/provider/openai ./internal/provider/anthropic ./internal/provider/costwrap -count=1`.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds test fixture stabilization.

## Unit B1: Evidence FailureSignal
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/evidence/evidence.go`, `DeepSeek-Reasonix/internal/evidence/evidence_test.go`
- **Depends on**: 无
- **Result**: `reasonix/internal/evidence` passed in focused and broad internal test runs.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit B2: UpgradePolicy and agent routing loop
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/agent/upgrade.go`, `DeepSeek-Reasonix/internal/agent/upgrade_test.go`, `DeepSeek-Reasonix/internal/agent/agent.go`, `DeepSeek-Reasonix/internal/agent/testutil/mock_provider.go`
- **Depends on**: Unit B1
- **Result**: `reasonix/internal/agent` full package passed after local SSE fixtures were isolated from machine proxy settings.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds test fixture stabilization.

## Unit B3: AgentConfig and boot wiring
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/config/config.go`, `DeepSeek-Reasonix/internal/config/default_test.go`, `DeepSeek-Reasonix/internal/boot/boot.go`, `DeepSeek-Reasonix/internal/boot/boot_test.go`, `DeepSeek-Reasonix/reasonix.example.toml`
- **Depends on**: Unit B2, Unit B4, Unit B5
- **Result**: `reasonix/internal/config` and `reasonix/internal/boot` full package tests pass; boot local provider tests now run direct against `httptest`.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds test fixture stabilization.

## Unit B4: Frontier provider cost wrapper
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/provider/costwrap/costwrap.go`, `DeepSeek-Reasonix/internal/provider/costwrap/costwrap_test.go`, `DeepSeek-Reasonix/internal/agent/agent.go`
- **Depends on**: 无
- **Result**: `reasonix/internal/provider/costwrap` passed standalone and in provider matrix; `reasonix/internal/agent` budget routing tests passed in full package.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit B5: Built-in advisor skill
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/skill/builtin_advisor.go`, `DeepSeek-Reasonix/internal/skill/builtins.go`, `DeepSeek-Reasonix/internal/skill/skill_test.go`, `DeepSeek-Reasonix/internal/boot/subagent_model_test.go`
- **Depends on**: 无
- **Result**: `reasonix/internal/skill` and `reasonix/internal/boot` full package tests pass, including advisor model precedence.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit B6: Runtime event kinds and UI wire-up
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/event/event_test.go`, `DeepSeek-Reasonix/internal/serve/wire.go`, `DeepSeek-Reasonix/internal/serve/wire_test.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/wire_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/components/*`, `DeepSeek-Reasonix/desktop/frontend/src/styles.css`
- **Depends on**: 无
- **Result**: `reasonix/internal/event`, `reasonix/internal/serve`, focused desktop wire tests, frontend `npm run typecheck`, and `npm run check:css` pass.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-A: Runtime skill injection store
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/skill/skill.go`, `DeepSeek-Reasonix/internal/skill/skill_test.go`
- **Depends on**: 无
- **Result**: `reasonix/internal/skill` full package tests pass, including injected skill read/list/remove coverage.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-B: Dynamic skill validator
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/skill/validator.go`, `DeepSeek-Reasonix/internal/skill/skill_test.go`
- **Depends on**: 无
- **Result**: `reasonix/internal/skill` full package tests pass, including validator accept/reject paths and high-risk task rejection.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-C: Skill matcher and dynamic generator
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/skill/matcher.go`, `DeepSeek-Reasonix/internal/skill/generator.go`, `DeepSeek-Reasonix/internal/skill/orchestrator.go`, `DeepSeek-Reasonix/internal/skill/skill_test.go`
- **Depends on**: Unit C1-A, Unit C1-B
- **Result**: `reasonix/internal/skill` full package tests pass, including generator retry, existing-skill matching, dynamic generation, and high-risk skip behavior.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C1-D: Controller orchestration integration
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/internal/control/input.go`, `DeepSeek-Reasonix/internal/control/input_test.go`, `DeepSeek-Reasonix/internal/control/controller_test.go`, `DeepSeek-Reasonix/internal/boot/boot.go`
- **Depends on**: Unit C1-C
- **Result**: `reasonix/internal/control` and `reasonix/internal/boot` full package tests pass, including runtime orchestration hint integration.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-A: Replay bundle capture
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/eval/replay.go`, `DeepSeek-Reasonix/internal/eval/eval_test.go`, `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/internal/control/controller_test.go`, `DeepSeek-Reasonix/internal/cli/run_metrics.go`, `DeepSeek-Reasonix/internal/cli/run_metrics_test.go`
- **Depends on**: Unit C1-D
- **Result**: `reasonix/internal/eval`, `reasonix/internal/control`, and focused CLI eval/run-metrics tests pass.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-B: Replay runner
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/eval/runner.go`, `DeepSeek-Reasonix/internal/eval/eval_test.go`
- **Depends on**: Unit C2-A
- **Result**: `reasonix/internal/eval` full package tests pass, including replay runner coverage.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-C: Frontier scorer
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/eval/scorer.go`, `DeepSeek-Reasonix/internal/eval/eval_test.go`
- **Depends on**: Unit C2-B
- **Result**: `reasonix/internal/eval` full package tests pass, including scorer parsing/fallback coverage.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit C2-D: Guardrail and skill promotion
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/eval/guardrail.go`, `DeepSeek-Reasonix/internal/eval/promote.go`, `DeepSeek-Reasonix/internal/eval/eval_test.go`, `DeepSeek-Reasonix/internal/cli/eval_cli.go`, `DeepSeek-Reasonix/internal/cli/eval_cli_test.go`
- **Depends on**: Unit C2-C
- **Result**: `reasonix/internal/eval` full package tests and focused `reasonix/internal/cli` eval guard/promote tests pass.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`

## Unit E2E: Cross-stage runtime verification
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/agent/upgrade_test.go`, `DeepSeek-Reasonix/internal/control/input_test.go`, `DeepSeek-Reasonix/internal/control/controller_test.go`, `DeepSeek-Reasonix/internal/cli/eval_cli_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/bridge.ts`, `DeepSeek-Reasonix/desktop/frontend/src/App.tsx`
- **Depends on**: Unit B3, Unit B6, Unit C1-D, Unit C2-D
- **Result**: Cross-stage focused Go tests, frontend typecheck, CSS checks, and previously captured real CLI/runtime-preview E2E validate the plan path end to end.
- **Commit**: `a396d09a feat: implement maddog fusion runtime`; current branch adds validation fixture stabilization and persistent task state.

## Unit B7: Automatic advisor consultation budget and context curation
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/agent/upgrade.go`, `DeepSeek-Reasonix/internal/agent/advisor.go`, `DeepSeek-Reasonix/internal/agent/agent.go`, `DeepSeek-Reasonix/internal/agent/upgrade_test.go`, `DeepSeek-Reasonix/internal/config/config.go`, `DeepSeek-Reasonix/internal/config/default_test.go`, `DeepSeek-Reasonix/internal/boot/boot.go`, `DeepSeek-Reasonix/internal/skill/builtin_advisor.go`
- **Depends on**: Unit B2, Unit B5, Unit B6
- **Started-at-commit**: a5844225
- **Result**: Automatic upgrade decisions now trigger a Go-native advisor consultation before frontier routing, with per-turn/session budgets, curated failure context, structured `event.Advisor`, and frontier-visible guidance. Verified by `go test ./internal/agent ./internal/config ./internal/provider/anthropic ./internal/boot ./internal/serve ./internal/cli -count=1` and full `go test ./... -count=1`.
- **Commit**: pending

## Unit B8: Anthropic native advisor tool support
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/provider/provider.go`, `DeepSeek-Reasonix/internal/provider/anthropic/anthropic.go`, `DeepSeek-Reasonix/internal/provider/anthropic/anthropic_test.go`
- **Depends on**: Unit B7
- **Result**: Added opt-in provider-native advisor config, Anthropic beta header/tool schema support, native server-block preservation/replay, and request exposure tests. Native advisor remains disabled by default. Verified by `go test ./internal/provider ./internal/provider/anthropic -count=1` within full `go test ./... -count=1`.
- **Commit**: pending

## Unit B9: Desktop advisor event presentation
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/event/event_test.go`, `DeepSeek-Reasonix/internal/serve/wire.go`, `DeepSeek-Reasonix/internal/serve/wire_test.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/wire_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/components/Message.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/Transcript.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/styles.css`
- **Depends on**: Unit B7
- **Result**: Advisor events now serialize through serve and Wails wire contracts, render as a dedicated desktop transcript card with reason/question/advice/budget metadata, export to Markdown, and remain visible in CLI/TUI sinks. Verified by `go test ./internal/serve -count=1`, `go test . -count=1` in `DeepSeek-Reasonix/desktop`, plus `npm run typecheck`, `npm run check:css`, and `npm run build`.
- **Commit**: pending

# Current Active Backlog

This backlog supersedes the old A/B/C execution plan for new development. It tracks the loop-governed four-scheme plan in `docs/cc/maddog-fusion--3949/plan-external-schemes.md`.

## Execution Order

`L0 -> D1 -> L1 -> D3 -> L2 -> E1 -> E2 -> E3 -> F1 -> F2 -> F3 -> G1 -> G2 -> G3 -> G4 -> L3 -> D2 -> L4`

Rationale: first freeze loop schema and provider projection, then readiness and budget/run log, then context/code intelligence/skill evolution, then maker-checker/auth polish and the full desktop control surface.

Post-mainline candidate spikes from `external-candidate-review-2026-06-28.md` are tracked after L4. They do not block the v1 loop-governed four-scheme plan.

## Cross-Cutting Preconditions

- **Shared sanitizer**: `internal/safety` must sanitize provider headers, Authorization, API key, OAuth token, icodeeasy token, dotenv secrets, raw tool output, run log, replay bundle, and export.
- **Budget ledger v1**: default is per-run frontier hard cap; frontier, advisor, checker, dynamic skill generation, and replay judge must all reserve/debit through the same ledger.
- **Credential model v1**: support `api_key_env`, `bearer_token_env`, `official_auth_profile_id`; browser OAuth/device flow is explicitly post-v1.
- **MCP capability/risk enum**: use `read`, `write`, `network`, `git`, `credential`, `process`; readiness and human gates must test these values.
- **Maker-checker isolation**: same provider with different role/prompt is allowed in v1 but marked `weak isolation`; strong isolation requires different model or provider.

## P0 Loop Governance

## Unit L0: LoopTemplateV1 schema and built-in registry
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/internal/loop/template.go`, `DeepSeek-Reasonix/internal/loop/registry.go`, `DeepSeek-Reasonix/internal/loop/registry_test.go`, `DeepSeek-Reasonix/internal/config/config.go`, `DeepSeek-Reasonix/internal/config/default_test.go`, `DeepSeek-Reasonix/internal/cli/workflow.go`, `DeepSeek-Reasonix/internal/cli/workflow_test.go`, `DeepSeek-Reasonix/internal/cli/cli.go`, `DeepSeek-Reasonix/internal/cli/cli_test.go`, `DeepSeek-Reasonix/internal/i18n/messages_en.go`, `DeepSeek-Reasonix/internal/i18n/messages_zh.go`, `DeepSeek-Reasonix/internal/i18n/messages_zh_tw.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/workflow_templates_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/lib/bridge.ts`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/workflow-templates.test.ts`
- **Depends on**: none
- **Test focus**: built-in `coding-task`, `review-task`, `skill-improvement`; schema version validation; duplicate phase rejection; negative budget rejection; project `.maddog/loops/` override metadata.
- **Acceptance**: GUI/CLI can list templates and select `coding-task` as a launch template.
- **Result**: `LoopTemplateV1` schema, built-in registry, project `.maddog/loops/` override metadata, desktop workflow snapshot, frontend workflow fixture, and `maddog workflows list/show` CLI are implemented with Maddog-only storage names. Verified with focused Go tests for loop/config/boot/CLI, desktop `TestWorkflowTemplates`, frontend workflow tests, `npm run typecheck`, and `npm run build`.
- **Commit**: pending

## Unit D1: Provider profile and role projection
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/internal/config/config.go`, `DeepSeek-Reasonix/internal/config/edit.go`, `DeepSeek-Reasonix/internal/config/default_test.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/settings_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/settings-models.test.ts`
- **Depends on**: none
- **Test focus**: default/frontier/small/advisor/maker/checker role derivation from existing config; icodeeasy/OpenAI-compatible gateway detection; missing credential status without token leakage.
- **Acceptance**: Settings can show provider role, auth mode, credential status, budget eligibility, and model mapping without creating a second provider store.
- **Result**: Provider profile projection is derived from existing `ProviderEntry` and agent role model refs, with Settings/desktop/frontend fields for roles, role model mapping, auth mode, credential env/status, gateway, and budget eligibility. Verified with config provider profile tests, focused desktop Settings tests, frontend settings model test, `npm run typecheck`, and `npm run build`; no token values are serialized.
- **Commit**: pending

## Unit L1: ReadinessResult schema and pre-run gate
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/internal/loop/readiness.go`, `DeepSeek-Reasonix/internal/loop/readiness_test.go`, `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/serve/wire.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/SettingsPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/StatusBar.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/readiness-panel.test.ts`
- **Depends on**: Unit L0, Unit D1; Unit F1 enriches capability checks later.
- **Test focus**: `ready`, `warning`, `blocked`, `needs_approval`; missing credential/budget/log sink/kill switch/human gate; MCP capability mismatch; CLI/headless and desktop wire parity.
- **Acceptance**: blocked runs cannot start; warning runs are logged; readiness never exposes token values.
- **Result**: Added `loop.ReadinessResult`/`EvaluateReadiness`, controller pre-run gate hooks, `readiness` event kind, serve/desktop wire payloads, desktop `WorkflowReadiness*` bindings, frontend readiness contract/reducer/transcript/status-bar display, and no-secret readiness snapshots. Verified by focused loop/control/serve/desktop tests, frontend readiness test, `npm run typecheck`, CSS checks, and `npm run build`.
- **Commit**: pending

## Unit D3: Provider usage, budget, and status event
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/internal/provider/costwrap/costwrap.go`, `DeepSeek-Reasonix/internal/agent/agent.go`, `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/serve/wire.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/components/StatusBar.tsx`
- **Depends on**: Unit D1
- **Test focus**: usage/cost aggregation for default/small/frontier/advisor; rate/status/balance snapshots; wire payloads; no credential leakage.
- **Acceptance**: after a real or fake run, desktop can explain provider role, upgrade reason, token/cost, and remaining budget.
- **Result**: Added role-scoped `provider_status` events with cumulative usage/cost, small/default/frontier/advisor role coverage, frontier budget remaining, upgrade reason, serve/desktop wire parity, frontend reducer/transcript/status-bar display, and costwrap usage snapshots. Verified with focused Go tests for costwrap/agent/event/serve/desktop wire, frontend provider-status contract, `npm run typecheck`, `npm run check:css`, and `npm run build` (build completed with existing Vite chunk/dynamic-import warnings).
- **Commit**: pending

## Unit L2: LoopRun/RunLog, budget ledger, sanitizer, and kill switch
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/internal/loop/runlog.go`, `DeepSeek-Reasonix/internal/loop/budget.go`, `DeepSeek-Reasonix/internal/safety/redact.go`, `DeepSeek-Reasonix/internal/loop/runlog_test.go`, `DeepSeek-Reasonix/internal/loop/budget_test.go`, `DeepSeek-Reasonix/internal/safety/redact_test.go`, `DeepSeek-Reasonix/internal/agent/agent.go`, `DeepSeek-Reasonix/internal/provider/costwrap/costwrap.go`, `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/proc/kill_windows.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/StatusBar.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/run-report.test.ts`
- **Depends on**: Unit L0, Unit L1, Unit D1, Unit D3
- **Test focus**: `RunID/LoopID/TurnID/StepID`; `RunStarted`, `ProviderCallStarted`, `BudgetDebited`, `HumanGateRequested`, `KillSwitchTriggered`, `RunStopped`, `RunReportReady`; request reserve/debit; stream mid-flight cap; concurrent cap; retry cost; sanitizer snapshots; turn/loop/global stop for provider stream, MCP stdio, process tree, scheduler.
- **Acceptance**: run log and report are written under Maddog data paths, budget is a hard cap, and secrets never appear in log/replay/event/export snapshots.
- **Result**: Added shared `internal/safety` redaction, `loop.RunLog` JSONL/report lifecycle, concurrent `BudgetLedger`, run-report event/wire/frontend status contract, headless/interactive run log lifecycle, kill-switch log event, and request-time frontier hard-cap rejection in `costwrap`. Verified with focused Go tests for safety/loop/costwrap/control/serve/desktop wire, frontend run-report contract, `npm run typecheck`, `npm run check:css`, and `npm run build` (build completed with existing Vite chunk/dynamic-import warnings).
- **Commit**: pending

## Unit E1: Tool output compressor interface and deterministic strategy
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/context/compress.go`, `DeepSeek-Reasonix/internal/context/compress_test.go`, `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/internal/control/controller_test.go`, `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/serve/wire.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/components/Transcript.tsx`
- **Depends on**: none
- **Test focus**: no-op under threshold; deterministic head/tail and error extraction; raw ref preservation; compression metrics event; frontend badge.
- **Acceptance**: long tool output entering model context is reduced while GUI can still open the full raw output.
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Result**: Added `internal/context` compressor interface and deterministic head/tail/error strategy, model-facing tool output compression in agent execution, raw-display ToolResult events with compression metrics, serve/desktop wire payloads, frontend reducer storage, and compact ToolCard savings badge. Boot now enables the deterministic compressor for real executor runs with conservative thresholds. Verified with compressor tests, agent integration test, serve/desktop wire tests, frontend tool-compression contract, `npm run typecheck`, and `npm run check:css`.
- **Commit**: pending

## Unit E2: Shell/test/log compression and context metrics
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/context/shell.go`, `DeepSeek-Reasonix/internal/context/shell_test.go`, `DeepSeek-Reasonix/internal/context/metrics.go`, `DeepSeek-Reasonix/internal/context/metrics_test.go`, `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/ContextPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/ToolCard.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/context-panel-breakdown.test.ts`
- **Depends on**: Unit E1
- **Started-at-commit**: 173f83ed
- **Test focus**: `go test`, `npm test`, `npm run build`, `rg`, `git diff/status`, server log summaries; repeated log dedupe; file:line preservation; empty summary fallback.
- **Acceptance**: model receives high-signal summaries and UI shows compression savings plus raw output access.
- **Result**: Added deterministic shell/log summarization and cumulative compression metrics, surfaced compression savings in the context panel, and locked the display contract with a frontend red/green test. Verification: `go test ./internal/context ./internal/agent ./internal/control -run 'TestSummarizeShellOutput|TestCompressionMetrics|TestDeterministicCompressor|TestToolOutputCompressor|TestContext' -count=1`; `npm exec -- tsx src/__tests__/context-panel-breakdown.test.ts`; `npm run typecheck`; `npm run check:css`.
- **Commit**: pending final commit

## Unit E3: Context policy, raw-data externalization, and export safety
- **Status**: completed
- **Execution note**: test-first
- **Files**: `DeepSeek-Reasonix/internal/config/config.go`, `DeepSeek-Reasonix/internal/config/edit.go`, `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/internal/control/controller_test.go`, `DeepSeek-Reasonix/internal/cli/run_metrics.go`, `DeepSeek-Reasonix/internal/cli/run_metrics_test.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/settings_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/SettingsPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/locales/en.ts`, `DeepSeek-Reasonix/desktop/frontend/src/locales/zh.ts`
- **Depends on**: Unit E1, Unit E2, Unit L2 sanitizer
- **Started-at-commit**: 173f83ed
- **Test focus**: policy off/auto/aggressive; raw missing; raw write failure; export with compression metadata, redacted snapshot, raw availability; no accidental raw blob in replay/export.
- **Acceptance**: users can control compression, resume/export remains consistent, and raw data is externalized safely.
- **Result**: Added `agent.context_policy` with `off|auto|aggressive`, session/cache-scoped `raw://tool-output/...` file raw store, compressor raw availability/error metadata, controller on-demand raw retrieval with compressed-summary fallback, metrics aggregation that excludes raw blobs/refs, boot policy wiring, and desktop Settings controls/locales. Verified by focused Go tests for config/context/control/CLI/boot, desktop Settings wire test, frontend `npm run typecheck`, and `npm run check:css`. A broader related-package run still has pre-existing non-E3 failures in `internal/agent` frontier fallback and `internal/cli` legacy/cwd tests.
- **Commit**: pending final commit

## Unit F1: Code intelligence backend registry and capability/risk enum
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed
- **Files**: `DeepSeek-Reasonix/internal/codegraph/backend.go`, `DeepSeek-Reasonix/internal/codegraph/backend_test.go`, `DeepSeek-Reasonix/internal/codegraph/codegraph.go`, `DeepSeek-Reasonix/internal/config/config.go`, `DeepSeek-Reasonix/internal/control/codegraph_mcp_test.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/capabilities_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`
- **Depends on**: none
- **Test focus**: built-in CodeGraph default; external backend degraded fallback; invalid tool mapping; capability/risk enum `read/write/network/git/credential/process`; readiness blockage on unauthorized capability.
- **Acceptance**: users can see current code intelligence backend and risk level; external failure never breaks built-in fallback.
- **Result**: Added a code intelligence backend registry with built-in CodeGraph default metadata, external MCP backend declarations under `[codegraph].backends`, backend capabilities (`symbol_search`, `semantic_search`, `context_pack`, `graph_trace`, `edit_refactor`, `health`), loop risk mapping to `read/write/network/git/credential/process`, invalid tool mapping detection, external degraded fallback, backend metadata in desktop `CapabilitiesView`, and frontend wire types/default normalization. Verified with F1 red/green tests for `internal/codegraph`, MCP naming compatibility in `internal/control`, desktop `Capabilities().CodeBackends`, frontend `npm run typecheck`, CSS checks, and broader `go test ./internal/codegraph ./internal/config ./internal/control -count=1`. A broad desktop `TestCapabilities*` run still has an existing legacy `reasonix.toml` Context7 failure outside F1.
- **Commit**: pending final commit

## Unit F2: Code intelligence benchmark harness
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/codegraph/bench.go`, `DeepSeek-Reasonix/internal/codegraph/bench_test.go`, `DeepSeek-Reasonix/cmd/codeintelbench/main.go`, `DeepSeek-Reasonix/internal/doctor/report.go`, `DeepSeek-Reasonix/internal/doctor/report_test.go`
- **Depends on**: Unit F1
- **Test focus**: mock and built-in backend reports; unsupported semantic search; query failure accounting; doctor summary path.
- **Acceptance**: benchmark produces JSON and markdown summaries for at least built-in and mock backend without external network dependency.
- **Result**: Added offline code intelligence benchmark harness with pluggable `BenchmarkBackend`, built-in local CodeGraph-style scanner, mock backend, JSON/Markdown report writers, latest-report cache for doctor, and `cmd/codeintelbench`. Unsupported semantic search is reported as unsupported, query failures are counted without aborting, and doctor surfaces only the recent summary path/backend health/failure counts. Verified by `go test ./internal/codegraph ./internal/doctor ./cmd/codeintelbench -count=1` plus `go run ./cmd/codeintelbench -repo . -out <tmp>/report.md -json <tmp>/report.json -latest=false`.
- **Commit**: pending final commit

## Unit F3: MCP code backend GUI management
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/capabilities_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/CapabilitiesPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/lib/bridge.ts`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/locales/en.ts`, `DeepSeek-Reasonix/desktop/frontend/src/locales/zh.ts`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/capabilities-panel.test.ts`
- **Depends on**: Unit F1, Unit F2
- **Test focus**: built-in/external status; retry health; async benchmark; capability/risk labels; credential/process enable confirmation; controller rebuild stability.
- **Acceptance**: GUI can manage code intelligence capabilities without exposing users only to raw MCP server lists.
- **Result**: Added desktop bindings for code backend enable/disable, health retry, and offline benchmark execution; Capabilities now carries latest benchmark summaries. The Capabilities drawer and Settings MCP page show a dedicated Code Intelligence group with health, capability/risk labels, tool mapping count, benchmark summary, retry, benchmark, and toggle controls with credential/process confirmation. Browser mock bridge and frontend types cover built-in plus external backend flows. Verified by focused desktop tests, `npm exec -- tsx src/__tests__/capabilities-panel.test.ts`, `npm run typecheck`, and `npm run check:css`.
- **Commit**: pending final commit

## Unit G1: Replay eval bundle v2 and candidate lifecycle
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/skilleval/bundle.go`, `DeepSeek-Reasonix/internal/skilleval/bundle_test.go`, `DeepSeek-Reasonix/internal/skilleval/candidate.go`, `DeepSeek-Reasonix/internal/skilleval/candidate_test.go`, `DeepSeek-Reasonix/internal/control/controller.go`, `DeepSeek-Reasonix/internal/control/input_test.go`, `DeepSeek-Reasonix/internal/skill/skill.go`, `DeepSeek-Reasonix/internal/event/event.go`
- **Depends on**: Unit E1 optional; Unit L2 sanitizer required
- **Test focus**: bundle with redacted snapshot and raw ref metadata; candidate hash dedupe; pure chat low-confidence bundle; validator rejection; secret-free bundle/candidate snapshots; `SkillGenerated` to bundle id association.
- **Acceptance**: dynamic skills create auditable pending candidates and never overwrite active skills directly.
- **Result**: Added `internal/skilleval` bundle/candidate v2 snapshots with shared redaction, project `.maddog/skilleval` JSON persistence, content-hash dedupe, rejected-candidate audit, and `SkillGenerated` bundle/candidate metadata through event and serve/desktop wire contracts. Runtime-generated skills now create pending candidates and no longer inject/override active skills before promotion. Verified by `go test ./internal/skilleval ./internal/skill ./internal/control ./internal/event ./internal/serve -count=1`, focused desktop wire tests, and frontend `npm run typecheck`.
- **Commit**: pending final commit

## Unit G2: Replay runner, guardrail, and promotion scoring
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/skilleval/runner.go`, `DeepSeek-Reasonix/internal/skilleval/runner_test.go`, `DeepSeek-Reasonix/internal/skilleval/scorer.go`, `DeepSeek-Reasonix/internal/skilleval/scorer_test.go`, `DeepSeek-Reasonix/internal/skilleval/guardrail.go`, `DeepSeek-Reasonix/internal/skilleval/guardrail_test.go`, `DeepSeek-Reasonix/internal/cli/skilleval.go`, `DeepSeek-Reasonix/internal/cli/skilleval_test.go`
- **Depends on**: Unit G1, Unit L2 budget ledger for optional frontier scorer
- **Test focus**: deterministic score fallback; pass-rate improvement; token reduction review-needed state; insufficient held-out bundles; allowed-tools expansion rejection; frontier scorer unavailable fallback.
- **Acceptance**: only candidates that pass replay and guardrail can become promotable.
- **Result**: Added offline skilleval replay summaries, deterministic scoring with frontier-unavailable fallback, guardrails for validator rejection/held-out coverage/allowed-tools expansion/pass-rate regression, candidate evaluation persistence, and `maddog skilleval list/evaluate`. Verified by `go test ./internal/skilleval -count=1` and focused CLI skilleval dispatch tests.
- **Commit**: pending final commit

## Unit G3: Skill management GUI and promotion audit
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/skills_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/CapabilitiesPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/MemoryPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/styles.css`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/skills-panel.test.ts`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/memory-suggestions.test.ts`
- **Depends on**: Unit G1, Unit G2
- **Test focus**: status filters active/disabled/dynamic/pending/promoted/rejected; candidate detail; promote/reject/rollback audit; write failure keeps pending; controller rebuild shows promoted skill.
- **Acceptance**: users can complete skill-evolution review in GUI and every decision is traceable.
- **Result**: Capabilities now includes skill candidates from project `.maddog/skilleval`; desktop bindings can promote, reject, and rollback candidates with JSONL audit records. Promotion writes a canonical project skill only after a promotable evaluation, keeps write failures pending, and Capabilities merges disk-discovered skills so promoted skills appear immediately. The Skills drawer renders candidate detail/actions and the mock bridge covers promote/reject/rollback. Verified by focused desktop skill tests, `npm exec -- tsx src/__tests__/skills-panel.test.ts`, `npm run typecheck`, and `npm run check:css`.
- **Commit**: pending final commit

## Unit G4: Rules plus LLM code review skill
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/review/rules.go`, `DeepSeek-Reasonix/internal/review/rules_test.go`, `DeepSeek-Reasonix/internal/review/report.go`, `DeepSeek-Reasonix/internal/review/report_test.go`, `DeepSeek-Reasonix/internal/skill/builtins.go`, `DeepSeek-Reasonix/internal/skill/skill_test.go`
- **Depends on**: Unit F1; Unit G1 optional for review bundle persistence
- **Test focus**: secret-like strings; unsafe shell; destructive SQL; large diff summary; missing code backend diff-only fallback; deterministic no-finding summary.
- **Acceptance**: review output is stable, explainable, and does not fail when code backend is degraded.
- **Result**: Added `internal/review` deterministic diff analysis for secret-like strings, unsafe shell execution, destructive SQL, large diff summaries, diff-only fallback metadata, and stable no-finding summaries. Added built-in `code-review` subagent skill that combines deterministic rules with LLM review judgment and documents codegraph degradation fallback. Verified by `go test ./internal/review ./internal/skill -count=1`.
- **Commit**: pending final commit

## Unit L3: Maker-checker execution contract and human gate
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/loop/maker_checker.go`, `DeepSeek-Reasonix/internal/loop/maker_checker_test.go`, `DeepSeek-Reasonix/internal/agent/task.go`, `DeepSeek-Reasonix/internal/skill/builtins.go`, `DeepSeek-Reasonix/internal/event/event.go`, `DeepSeek-Reasonix/internal/serve/wire.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/Transcript.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/maker-checker.test.ts`
- **Depends on**: Unit L0, Unit L1, Unit L2, Unit D1, Unit G4
- **Test focus**: `off`, `review_only`, `enforced_before_done`; read-only checker; weak/strong isolation display; changes-requested single retry; blocked/needs-human verdict; human gates for git push, deletion, credential change, budget increase, skill promotion.
- **Acceptance**: with enforced mode enabled, a run cannot complete without an approved checker verdict or explicit human decision.
- **Result**: Added `loop.MakerCheckerResult`/`HumanGateResult` evaluators, strong/weak isolation reporting, retry/human-gate decisions for enforced checker verdicts, `maker_checker`/`human_gate` event and serve/desktop wire payloads, frontend transcript rendering, and read-only `code-review` checker tools. Verified by focused Go tests for loop/serve/skill/desktop wire, frontend maker-checker contract, `npm run typecheck`, and `npm run check:css`.
- **Commit**: pending final commit

## Unit D2: Official auth, API key, and icodeeasy configuration
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/internal/provider/auth.go`, `DeepSeek-Reasonix/internal/provider/openai/fetch_models.go`, `DeepSeek-Reasonix/internal/provider/openai/fetch_models_test.go`, `DeepSeek-Reasonix/internal/provider/anthropic/anthropic_test.go`, `DeepSeek-Reasonix/internal/provider/provider_test.go`, `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/settings_app_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/SettingsPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/lib/providerModels.ts`, `DeepSeek-Reasonix/desktop/frontend/src/locales/en.ts`, `DeepSeek-Reasonix/desktop/frontend/src/locales/zh.ts`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/settings-provider-auth.test.ts`
- **Depends on**: Unit D1; credential model v1 frozen in Cross-Cutting Preconditions
- **Test focus**: `api_key_env`, `bearer_token_env`, `official_auth_profile_id`; icodeeasy/OpenAI-compatible base URL; 401/403/500/timeout classification; no token in Settings JSON or doctor output.
- **Acceptance**: GUI can configure OpenAI/Anthropic official/API key/icodeeasy flows, probe models, and save role mappings without leaking credentials.
- **Result**: Added credential model v1 fields (`bearer_token_env`, `official_auth_profile_id`) with legacy `auth_token_env` alias compatibility, official auth mode projection, provider model-probe auth parity, 401/403 vs provider-unavailable classification, redacted probe errors, doctor auth diagnostics, and Settings/GUI support for API key, bearer/official token, workload identity, official profile id, and icodeeasy/OpenAI-compatible probes. Verified by focused Go tests for provider/config/doctor/desktop Settings, frontend `settings-provider-auth`, `settings-models`, `provider-status`, `npm run typecheck`, and `npm run check:css`.
- **Commit**: pending final commit

## Unit L4: Desktop Loop Control Surface and run reports
- **Status**: completed
- **Execution note**: test-first
- **Started-at-commit**: 173f83ed1f16ad48df2a34b6d34a57127dc9745b
- **Files**: `DeepSeek-Reasonix/desktop/app.go`, `DeepSeek-Reasonix/desktop/wire.go`, `DeepSeek-Reasonix/desktop/settings_app_test.go`, `DeepSeek-Reasonix/desktop/wire_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/SettingsPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/CapabilitiesPanel.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/StatusBar.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/components/Transcript.tsx`, `DeepSeek-Reasonix/desktop/frontend/src/lib/types.ts`, `DeepSeek-Reasonix/desktop/frontend/src/styles.css`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/loop-control-surface.test.ts`
- **Depends on**: Unit L0-L3, Unit D1-D3, Unit F1, Unit G1
- **Test focus**: Settings -> Workflows; Task Start readiness; Live Controls; History/Telemetry run reports; provider/model role display; frontier upgrade reason; pending human gate survives refresh/restart.
- **Acceptance**: users can configure, select, start, monitor, and audit a `coding-task` loop from GUI without editing TOML.
- **Result**: Added Settings -> Workflows with template/readiness refresh, structured run report JSON persistence, detailed run report wire assertions, live reducer state for pending human gates and maker-checker verdicts, transcript/status-bar/Markdown report display for template/model/budget/checker/gate details, and Maddog-only visible locale/config wording. Verified with focused Go tests for loop/control/event/serve/config/desktop, frontend workflow/readiness/provider/maker-checker/run-report contracts, `npm run typecheck`, `npm run check:css`, and `npm run build` (Vite retained existing chunk/dynamic-import warnings).
- **Commit**: pending final commit

## Post-Mainline Candidate Spikes

These tasks come from the 2026-06-28 expert review of FastContext, zvec, ultracode-skill, harness-1, and Iterative-Contextual-Refinements. They are useful follow-up work, but they must not change the current v1 execution order or become hidden runtime dependencies.

## Unit F4: FastContext-style Repository Explorer Benchmark
- **Status**: completed
- **Execution note**: spike/test-first
- **Files**: `DeepSeek-Reasonix/internal/codegraph/bench.go`, `DeepSeek-Reasonix/internal/codegraph/bench_test.go`, `DeepSeek-Reasonix/internal/codegraph/backend.go`, `DeepSeek-Reasonix/cmd/codeintelbench/main.go`, `DeepSeek-Reasonix/desktop/frontend/src/components/CapabilitiesPanel.tsx`
- **Depends on**: Unit F1, Unit F2, Unit F3, Unit L4
- **Test focus**: compact exploration trace; file-line citation preservation; citation precision fixture; token chars returned; query latency; fallback to built-in CodeGraph; benchmark summary visibility in GUI.
- **Acceptance**: a FastContext-style explorer can be evaluated beside built-in/mock backends without embedding the external Python runner or changing the default backend.
- **Result**: Added citation precision and compact exploration trace metrics to the offline code intelligence benchmark, surfaced citation precision through doctor/desktop benchmark summaries and the Capabilities GUI, and kept FastContext-style evaluation optional beside built-in/mock backends. Verified by `go test ./internal/codegraph -run 'TestRunBenchmark|TestZvecHybridStoreAssessmentIsOptionalAndRiskGated' -count=1`, desktop `TestRunCodeBackendBenchmarkStoresSummary`, and frontend `capabilities-panel` contract tests.
- **Commit**: pending final commit

## Unit F5: zvec Hybrid Store Spike
- **Status**: completed
- **Execution note**: spike/test-first
- **Files**: `DeepSeek-Reasonix/internal/codegraph/backend.go`, `DeepSeek-Reasonix/internal/codegraph/bench.go`, `DeepSeek-Reasonix/internal/context/metrics.go`, `DeepSeek-Reasonix/internal/doctor/report.go`
- **Depends on**: Unit F1, Unit F2, Unit E3, Unit L4
- **Test focus**: dense/sparse/FTS/hybrid capability mapping; Windows packaging risk; WAL and index migration behavior; incremental update; concurrent write handling; embedding pipeline boundary; degraded fallback.
- **Acceptance**: decide whether zvec is viable as an optional local vector/hybrid backend, with no new v1 hard dependency.
- **Result**: Added default-off zvec hybrid-store assessment with dense/sparse/FTS/hybrid/WAL capabilities, Windows packaging/WAL/migration/concurrency/embedding checks, degraded built-in fallback, and doctor visibility. The assessment keeps zvec optional and not a v1 hard dependency. Verified by `go test ./internal/codegraph -run 'TestRunBenchmark|TestZvecHybridStoreAssessmentIsOptionalAndRiskGated' -count=1` and `go test ./internal/doctor -run 'TestCollectReportIncludesHybridStoreSpikeAssessment|TestCollectReportIncludesRecentCodeintelBenchmarkSummary' -count=1`.
- **Commit**: pending final commit

## Unit L5: Ultracode-style Workflow Artifact Review
- **Status**: completed
- **Execution note**: documentation/template spike
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/loop/template.go`, `DeepSeek-Reasonix/internal/loop/registry_test.go`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/workflow-templates.test.ts`, `docs/cc/maddog-fusion--3949/external-candidate-review-2026-06-28.md`
- **Depends on**: Unit L0, Unit L4
- **Test focus**: task packet fields; bounded fan-out metadata; delegation artifacts; integration checklist; final verification artifact; run report mapping.
- **Acceptance**: identify which workflow artifact fields should become Maddog template/report fields without copying ultracode-skill prompts or runtime conventions.
- **Result**: Added Maddog-native workflow artifact contract metadata to `LoopTemplateV1` and built-in templates, including task packet fields, bounded fan-out, delegation artifacts, integration checklist, final verification artifacts, and run report mapping. Desktop `WorkflowTemplateView`, mock bridge data, workflow Settings display, and candidate review docs now expose the same contract without copying ultracode prompts or runtime conventions. Verified by focused `internal/loop`, desktop workflow template, `settings-workflows`, and `workflow-templates` tests.
- **Commit**: pending final commit

## Unit G5: Long-Horizon Eval Harness Research
- **Status**: completed
- **Execution note**: research/spike
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/skilleval/bundle.go`, `DeepSeek-Reasonix/internal/skilleval/runner.go`, `DeepSeek-Reasonix/internal/skilleval/scorer.go`, `DeepSeek-Reasonix/internal/loop/runlog.go`
- **Depends on**: Unit G1, Unit G2, Unit L2, Unit L4
- **Test focus**: candidate docs; curated evidence; verification records; action/observation trajectory export; budget-aware replay context; deterministic failure accounting.
- **Acceptance**: produce a concrete eval harness proposal for Maddog replay/skilleval without importing harness-1 training, CUDA, vLLM, checkpoint, or model-serving stack.
- **Result**: Added long-horizon evidence structures to skilleval bundles, including candidate docs, curated evidence, verification records, action/observation trajectory, and budget-aware replay context. Replay can now generate a `long-horizon-v1` harness proposal with evidence counts, budget totals, deterministic failure accounting, and explicit exclusion of training/CUDA/vLLM/checkpoint/model-serving runtime dependencies; run reports can persist the proposal summary. Verified by focused skilleval bundle/replay tests and loop runlog report tests.
- **Commit**: pending final commit

## Unit L3b: Iterative Refinement Strategy Templates
- **Status**: completed
- **Execution note**: post-v1 strategy spike
- **Started-at-commit**: 173f83ed1f16
- **Files**: `DeepSeek-Reasonix/internal/loop/maker_checker.go`, `DeepSeek-Reasonix/internal/loop/template.go`, `DeepSeek-Reasonix/internal/loop/budget.go`, `DeepSeek-Reasonix/desktop/frontend/src/__tests__/maker-checker.test.ts`
- **Depends on**: Unit L3, Unit L4
- **Test focus**: BFS/DFS-like hypothesis exploration as template metadata; critique/correction rounds; final judge isolation; budget cap; kill switch; human approval before expensive deep refinement.
- **Acceptance**: define a default-off deep refinement template that can be audited and stopped, without making ICR-style search the standard coding-task loop.
- **Result**: Added default-off `refinementStrategy` metadata to workflow templates with BFS-hypothesis and DFS-correction modes, critique/correction round caps, strong final-judge isolation, token budget cap, kill-switch requirement, and human approval requirement. Added `EvaluateRefinementStrategy` so deep refinement remains off unless explicitly enabled and gated by budget, kill switch, and human approval; desktop workflow views and frontend workflow/maker-checker contracts expose the strategy. Verified by focused loop registry/evaluator tests, desktop workflow template tests, and frontend maker-checker/workflow template tests.
- **Commit**: pending final commit

## Final Verification - 2026-06-29
- **Status**: completed
- **Scope**: Completed plan-level validation after Maddog naming/storage/desktop artifact cleanup.
- **Go validation**: `go test ./... -count=1 -timeout 300s` passed in the root module; `go test . -count=1 -timeout 240s` passed in `DeepSeek-Reasonix/desktop`; `go test . -count=1 -timeout 180s` passed in `DeepSeek-Reasonix/desktop/cmd/sign`.
- **Frontend validation**: `npm run typecheck`, `npm run check:css`, `npm run test:all`, all 38 `desktop/frontend/src/__tests__/*.test.ts`, and `npm run build` passed.
- **Site/worker validation**: `npm ci`, `npm run typecheck`, and `npm audit --audit-level=high` passed in `workers/crash-report` after lockfile audit remediation (`wrangler 4.105.0`, `miniflare 4.20260625.0`, `undici 7.28.0`, 0 vulnerabilities); `npm ci` and `npm run build` passed in `site`.
- **Desktop artifact**: Wails build produced `DeepSeek-Reasonix/desktop/build/bin/maddog-dev.exe` (40,179,712 bytes). Smoke launch exited 0 while another Maddog desktop process was already running, consistent with single-instance behavior; no user process was killed.
- **Naming/storage audit**: actionable `Reasonix`/`reasonix`/`.reasonix` remnants are 0 after excluding Go module/import paths, legacy migration/isolation tests, and intentional legacy safety patterns. Desktop state is under `%APPDATA%/maddog-dev` with `sessions`, `global`, `global-workspace`, and desktop state files present.
- **Commit**: pending final commit
