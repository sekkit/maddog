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
