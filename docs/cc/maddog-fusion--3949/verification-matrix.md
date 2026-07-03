---
slugid: maddog-fusion--3949
stage: verification
date: 2026-07-03
source_spec: docs/cc/maddog-fusion--3949/spec.md
source_plan: docs/cc/maddog-fusion--3949/plan.md
source_tasks: docs/cc/maddog-fusion--3949/tasks.md
source_external_plan: docs/cc/maddog-fusion--3949/plan-external-schemes.md
---

# Maddog Fusion Verification Matrix

This file is the working audit map for the full Maddog fusion goal: every feature group, its implementation units, the GitHub project or paper references that informed it, and the concrete CLI/GUI task that should trigger it.

## Feature And Reference Matrix

| Feature group | Units | Local surfaces | GitHub / paper references | Trigger task |
| --- | --- | --- | --- | --- |
| Multi-provider acceptance | A1 | OpenAI-compatible and Anthropic providers, model registry, provider switching | Maddog-native baseline; DeepSeek/OpenAI-compatible and Anthropic protocols from `spec.md`; tinyctx is only a contrast point because its `/v1/responses` proxy is intentionally not reused | Configure fake or test providers, run chat and tool-call acceptance tests for both provider kinds |
| Failure signal and frontier upgrade | B1, B2, B3, B4, B7, B8 | `internal/evidence`, `internal/agent`, boot config, cost wrapper, native Anthropic advisor tool support | tinyctx router/advisor concepts from `spec.md`; [emanueleielo/advisor-middleware](https://github.com/emanueleielo/advisor-middleware); [cobusgreyling/loop-engineering](https://github.com/cobusgreyling/loop-engineering) | Force repeated tool/provider failures and verify visible upgrade reason, budget accounting, fallback, and advisor consultation |
| Advisor skill and runtime events | B5, B6, B9 | Built-in `advisor` skill, subagent routing, runtime event kinds, serve/desktop wire contracts | tinyctx `ask_advisor` contract from `spec.md`; [cobusgreyling/loop-engineering](https://github.com/cobusgreyling/loop-engineering) | Invoke `/advisor` or an automatic advisor path and verify CLI transcript, SSE/wire event, and desktop transcript presentation |
| Runtime skill orchestration | C1-A, C1-B, C1-C, C1-D | Skill injection store, dynamic skill validator, matcher/generator/orchestrator, controller integration | tinyctx `orchestration_injector` and `dynamic_skill` concepts from `spec.md`; existing Maddog `.maddog/skills` and `run_skill` surfaces | Submit a normal task with an existing skill match, an unmatched low-risk task, and a high-risk task; verify existing match, one-turn dynamic skill generation, and high-risk rejection |
| Offline replay self-improvement | C2-A, C2-B, C2-C, C2-D, G1, G2, G3 | Replay bundle capture, runner, scorer, guardrail, promotion, candidate store, desktop skill candidate UI | [microsoft/SkillOpt](https://github.com/microsoft/SkillOpt) and paper [arXiv:2605.23904](https://arxiv.org/abs/2605.23904); [iamzhihuix/skills-manage](https://github.com/iamzhihuix/skills-manage); [PabloNAX/ultracode-skill](https://github.com/PabloNAX/ultracode-skill); [pat-jj/harness-1](https://github.com/pat-jj/harness-1) | Capture or load a replay bundle, run `skilleval` dry-run/provider evaluation, verify guardrail outcome, candidate status, desktop promotion/rollback audit |
| Provider profile, auth, budget, status | D1, D2, D3 | Config render/edit/defaults, provider auth, usage/status events, Settings panel, StatusBar | [BerriAI/litellm](https://github.com/BerriAI/litellm); [aaif-goose/goose](https://github.com/aaif-goose/goose); [steipete/CodexBar](https://github.com/steipete/CodexBar); [emanueleielo/advisor-middleware](https://github.com/emanueleielo/advisor-middleware); [cobusgreyling/loop-engineering](https://github.com/cobusgreyling/loop-engineering) | Use a config with default/planner/frontier/small/advisor roles and auth modes; verify CLI diagnostics plus desktop Settings/StatusBar projection |
| Context compression and raw-result lookup | E1, E2, E3 | `internal/contextpack`, tool result compression, raw result store, config policy, desktop context controls | [headroomlabs-ai/headroom](https://github.com/headroomlabs-ai/headroom); [rtk-ai/rtk](https://github.com/rtk-ai/rtk); [mksglu/context-mode](https://github.com/mksglu/context-mode); [dirac-run/dirac](https://github.com/dirac-run/dirac); [microsoft/fastcontext](https://github.com/microsoft/fastcontext); [ryoiki-tokuiten/Iterative-Contextual-Refinements](https://github.com/ryoiki-tokuiten/Iterative-Contextual-Refinements) | Run shell/test/log/search outputs through the tool-result path; verify compressed model content, raw lookup availability, token delta events, and off/auto/aggressive policy |
| Code intelligence backend and benchmark | F1, F2, F3 | CodeGraph backend registry, optional MCP backend mapping, benchmark harness, Capabilities panel | [DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp); [oraios/serena](https://github.com/oraios/serena); [zilliztech/claude-context](https://github.com/zilliztech/claude-context); [alibaba/zvec](https://github.com/alibaba/zvec); [HyperGraphRAG](https://github.com/Graph-COM/HyperGraphRAG) and NeurIPS 2025 paper [HyperGraphRAG](https://proceedings.neurips.cc/paper_files/paper/2025/hash/df55ee6e59f8ac4a625219e11fe9ddba-Abstract-Conference.html) as graph/RAG research context | Configure built-in CodeGraph plus valid/invalid MCP code intelligence backends; run backend registry tests and benchmark command; verify desktop Capabilities status |
| Rule/LLM hybrid review | G4 | `internal/review`, built-in review skill, CLI review prompt generation | [alibaba/open-code-review](https://github.com/alibaba/open-code-review); [microsoft/SkillOpt](https://github.com/microsoft/SkillOpt) for replayable skill lifecycle | Run deterministic review rules on a diff and build the LLM review task; verify findings, prompt contract, and skill exposure |
| Cross-stage runtime integration | E2E | Agent upgrade loop, controller inputs, eval CLI, serve/SSE, desktop/frontend bridge | All references above; this unit proves the pieces compose inside the single Maddog app rather than only as isolated package features | Run the focused CLI chunks, full Go suite, desktop Go tests, frontend tests/build, and serve/SSE smoke |

## Task-Level Acceptance Metrics

| Area | Acceptance standard | Required artifact |
| --- | --- | --- |
| Multi-provider acceptance | At least one OpenAI-compatible provider path and one Anthropic provider path must pass transport, model resolution, tool-call, and boot routing tests without relying on real user credentials. | `go test ./internal/provider/... ./internal/boot -count=1`; fake/test provider fixtures in package tests. |
| Frontier upgrade and advisor loop | Default failure threshold remains `upgrade_threshold = 3`; frontier output-token budget remains visible and bounded by `frontier_budget`; advisor max use default remains 1 per turn; advisor runner is available when advisor is enabled even if frontier is not configured. | `go test ./internal/agent ./internal/boot -run 'Advisor|Upgrade|Frontier|SubagentModelRef' -count=1`; config render tests. |
| Offline replay self-improvement | Promotion-grade CLI evaluation must use at least 5 distinct held-out bundles by default, reject duplicate bundle IDs/paths, reject the candidate source bundle, require every score to be >= 0.70, reject success-rate regressions, and persist aggregate score/guardrail when `--store-dir` is supplied. | `go test ./internal/skilleval ./internal/cli -run 'SkillEval|Guardrail' -count=1`; JSON `skilleval` output with `bundles`, `bundle_ids`, `scores`, and `persisted`. |
| Offline replay GUI | The GUI must expose candidate evaluation, promotion, rollback, rejection, status, score, guardrail reason, source bundle, and audit metadata from Settings/Skills or Capabilities without weakening the default 5-bundle guardrail. | `go test . -run 'SkillCandidate|EvaluateSkillCandidate' -count=1` in `desktop`; `capabilities-skill-candidates.test.ts`. |
| Provider profile/status UI | Settings must show default/planner/frontier/small/advisor roles, auth mode, credential env, configured status, model list, and manual refresh controls; StatusBar must preserve provider role/model/budget snapshots from runtime events. | `go test . -run 'SettingsProviderProfiles|ProviderProfiles' -count=1`; `maddog-mechanisms-contract.test.ts`; `status-bar.test.ts`. |
| Context compression/raw lookup | Compression must expose raw/compressed/saved metrics; UI breakdown fixtures must demonstrate at least 50% token saving on a large compressed sample; raw lookup must show an unavailable note when the raw artifact is missing. | `go test ./internal/contextpack ./internal/history ./internal/control -run 'Compress|Raw|Tool|Context|Shell' -count=1`; `context-panel-breakdown.test.ts`; `tool-raw-unavailable.test.ts`. |
| Code intelligence benchmark | `cmd/codeintelbench` and `cmd/e2ebench` must build and pass package tests; desktop benchmark UI must label local smoke benchmark behavior clearly when not invoking a selected external backend. | `go test ./cmd/codeintelbench ./cmd/e2ebench -count=1`; `go test . -run 'RunCodeIntelligenceBenchmark' -count=1` in `desktop`. |
| Desktop visual and layout safety | Frontend TypeScript, CSS syntax, z-index token checks, command-palette shortcut, logo/wordmark layout, provider role chips, and production Vite build must pass. External Chrome/opencli screenshot smoke is preferred when a GUI server is running. | `npm run test:all`; `npm run build`; optional `opencli browser ...` screenshot artifacts. |
| Cross-stage regression | Full Go module regression, desktop Go tests, frontend tests/build, and serve/SSE smoke must pass in the same verification window. | `go test ./... -count=1 -timeout 240s`; `go test . -count=1 -timeout 240s` in `desktop`; serve/SSE HTTP smoke. |

## End-To-End Trigger Tasks

| Task ID | Purpose | Command or action | Covers | Expected evidence |
| --- | --- | --- | --- | --- |
| VT-CLI-0 | Build current CLI artifact | `go build -o bin/maddog-test.exe ./cmd/maddog` | All CLI-accessible surfaces | Binary exists and `maddog-test.exe --help` lists supported commands |
| VT-CLI-1 | Provider/advisor routing tests | `go test ./internal/provider/... ./internal/evidence ./internal/agent ./internal/boot -count=1` | A, B | Provider wire tests, failure signal, upgrade policy, cost wrapper, boot config pass |
| VT-CLI-2 | Runtime skill orchestration tests | `go test ./internal/skill ./internal/control -run 'Skill|Orchestrat|Advisor|Input|Slash' -count=1` | B, C | Skill store, validator, matcher/generator, slash/advisor/controller paths pass |
| VT-CLI-3 | Replay and skill eval CLI | `go test ./internal/eval ./internal/skilleval ./internal/cli -run 'Eval|SkillEval|RunMetrics|Review' -count=1` and `bin/maddog-test.exe skilleval -help` | C2, G | Replay/scoring/guardrail/promotion and CLI command contract pass |
| VT-CLI-4 | Context compression and raw lookup | `go test ./internal/contextpack ./internal/history ./internal/control -run 'Compress|Raw|Tool|Context|Shell' -count=1` | E | Compression summaries, raw externalization, and controller query paths pass |
| VT-CLI-5 | Code intelligence registry and benchmark | `go test ./internal/codegraph ./internal/config ./internal/control -run 'Code|Backend|MCP|HyperGraph' -count=1`; build/run `cmd/codeintelbench` if present | F | Built-in CodeGraph remains default, invalid MCP mapping is rejected, benchmark surfaces execute |
| VT-CLI-6 | Full Go regression | `go test ./... -count=1 -timeout 240s` | All Go kernel/CLI units | Whole module is green |
| VT-GUI-1 | Desktop Go bridge/contracts | In `desktop`: `go test . -count=1` | B6, B9, D, E, F, G3 | Wails app methods, wire conversion, Settings/Capabilities/StatusBar projections pass |
| VT-GUI-2 | Frontend unit and contract tests | In `desktop/frontend`: `npm run test:all` | B6, B9, D, E, F, G3 | React/TypeScript UI contract tests pass |
| VT-GUI-3 | Frontend production build | In `desktop/frontend`: `npm run build` | GUI bundle | CSS checks, TypeScript, and Vite build pass |
| VT-GUI-4 | Visual smoke | Start Vite or Wails dev, inspect with external Chrome via `opencli browser ...`, capture Settings, Capabilities, transcript/status screenshots | D, E, F, G3, B9 | No blank/overlapping UI; provider/context/code-intelligence/skill-candidate areas visible |
| VT-SRV-1 | Serve/SSE contract smoke | `bin/maddog-test.exe serve --addr 127.0.0.1:<port>` with isolated temp config, then request `/` and event endpoints | B6, B9, D, E | Server starts, browser client loads, events serialize without errors |

## Dependency Readiness

| Dependency | Status | Evidence |
| --- | --- | --- |
| Go toolchain | Ready | `go version go1.26.4 windows/amd64` from `C:\Dev2\.tools\go1.26.4\bin\go.exe`. |
| Wails | Ready with version warning | `wails version` reports `v2.11.0`; package build warns that `go.mod` uses Wails `2.12.0`, but the build still succeeds. |
| Node/npm/pnpm | Ready | Node `v22.17.1`, npm `10.9.2`, pnpm `11.5.2`. |
| context-mode | Ready | `ctx_doctor` passed server, FTS5/SQLite, hook, storage, and Codex CLI checks; only warning is optional Bun performance. |
| opencli external Chrome bridge | Ready | `opencli v1.8.5 doctor` reports daemon running, extension connected, profile connected, and connectivity OK. |
| Code intelligence benchmark commands | Ready | `cmd/codeintelbench` and `cmd/e2ebench` are present and package tests pass. |
| Browser visual smoke | Available but not captured in this pass | Browser Bridge is now connected; no Codex in-app browser was used. Start Vite or Wails dev and use external Chrome/opencli for screenshots when visual artifacts are required. |

## Current Evidence Log

| Date | Evidence | Result |
| --- | --- | --- |
| 2026-07-03 | `git pull --ff-only origin main` | Already up to date at `fa1f1b5e8cac` |
| 2026-07-03 | VT-CLI-0: `go build -o bin/maddog-test.exe ./cmd/maddog` | Pass |
| 2026-07-03 | VT-CLI-0: `bin/maddog-test.exe --help`, `skilleval --help`, `serve --help`, `doctor --help` | Pass for supported commands; `codeintelbench` and `skill` are not top-level `maddog` subcommands and must be verified via their actual binaries or package tests |
| 2026-07-03 | VT-CLI-1: `go test ./internal/provider/... ./internal/evidence ./internal/agent ./internal/boot -count=1 -timeout 240s` | Pass: provider, evidence, agent, boot |
| 2026-07-03 | VT-CLI-2: `go test ./internal/skill ./internal/control -run 'Skill|Orchestrat|Advisor|Input|Slash' -count=1 -timeout 240s` | Pass: runtime skill orchestration and controller/advisor slices |
| 2026-07-03 | VT-CLI-3: `go test ./internal/eval ./internal/skilleval ./internal/cli -run 'Eval|SkillEval|RunMetrics|Review' -count=1 -timeout 240s` | Pass: replay/skilleval/review CLI slices |
| 2026-07-03 | VT-CLI-4: `go test ./internal/contextpack ./internal/history ./internal/control -run 'Compress|Raw|Tool|Context|Shell' -count=1 -timeout 240s` | Pass: context compression/raw lookup slices |
| 2026-07-03 | VT-CLI-5: `go test ./internal/codegraph ./internal/config ./internal/control -run 'Code|Backend|MCP|HyperGraph' -count=1 -timeout 240s` | Pass: code intelligence registry/config/controller slices |
| 2026-07-03 | VT-CLI-5: `go build -o bin/codeintelbench-test.exe ./cmd/codeintelbench`; `go build -o bin/e2ebench-test.exe ./cmd/e2ebench`; `go test ./cmd/codeintelbench ./cmd/e2ebench -count=1 -timeout 240s` | Pass: benchmark entrypoints are separate binaries and package tests pass |
| 2026-07-03 | VT-CLI-6: `go test ./... -count=1 -timeout 240s` | Pass: 74 Go packages reported |
| 2026-07-03 | VT-GUI-1: In `desktop`: `go test . -count=1 -timeout 240s` | Pass: desktop bridge/app contracts |
| 2026-07-03 | VT-GUI-2: In `desktop/frontend`: `npm run test:all` | Pass: 0 failed; provider usage/status, context compression, code intelligence, and skill candidate UI contracts included |
| 2026-07-03 | VT-GUI-3: In `desktop/frontend`: `npm run build` | Pass: CSS checks, z-index check, TypeScript, and Vite build; Vite emitted non-fatal chunk-size/dynamic-import warnings |
| 2026-07-03 | VT-SRV-1: Outside-repo isolated serve smoke with dummy provider store: `maddog-test.exe serve --model smoke --addr 127.0.0.1:8798 --auth none`; requested `/`, `/history`, `/context`, `/status`, `/sessions`, `/skills`, `/todos`, `/branches`, `/checkpoints`, `/events` | Pass: browser client HTML and JSON endpoints returned 200; `/events` opened as `text/event-stream`; no provider key dependency on real user credentials |
| 2026-07-03 | Dependency check: `opencli doctor`; `ctx_doctor`; Go/Wails/Node/npm/pnpm versions | Pass: opencli daemon/extension/profile connected; context-mode server/FTS/hooks OK; Go `1.26.4`, Wails `2.11.0`, Node `22.17.1`, npm `10.9.2`, pnpm `11.5.2` available |
| 2026-07-03 | Focused fix verification: `go test ./internal/cli ./internal/boot ./internal/config -run "SkillEval|SubagentModelRef|Advisor|RemoveProvider|Render" -count=1` | Pass: held-out bundle validation, provider scoring/persistence, advisor model precedence/gate, config render/edit slices |
| 2026-07-03 | Focused desktop role verification: in `desktop`, `go test . -run "SettingsProviderProfiles|SetAdvisorModel|ProviderProfiles" -count=1` | Pass: provider views include advisor model and role annotations |
| 2026-07-03 | Frontend role/discoverability checks: `pnpm exec tsx src\__tests__\maddog-mechanisms-contract.test.ts`, `app-chrome-tabs.test.ts`, `capabilities-skill-candidates.test.ts`, and `pnpm exec tsc --noEmit -p tsconfig.test.json` | Pass: provider role chips contract, offline replay command-palette shortcut, logo/wordmark regressions, skill candidate UI, and TypeScript |
| 2026-07-03 | Dependency package checks: `go test ./internal/control -count=1 -timeout 240s`; `go test ./cmd/codeintelbench ./cmd/e2ebench -count=1 -timeout 240s` | Pass: previous Windows temp cleanup flake did not reproduce; benchmark commands pass |
| 2026-07-03 | Wider regression after fixes: `go test ./... -count=1 -timeout 240s`; in `desktop`, `go test . -count=1 -timeout 240s`; in `desktop/frontend`, `npm run test:all` and `npm run build` | Pass: full Go module, desktop bridge/app tests, frontend test suite, and production build. Vite warnings are non-fatal chunk/dynamic-import/plugin timing warnings. |
| 2026-07-03 | Package build without config reset: in inner repo, `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build-package-run-maddog.ps1 -GoExe C:\Dev2\.tools\go1.26.4\bin\go.exe -WailsExe C:\Users\Sekkit\go\bin\wails.exe -NoLaunch -NoClean` | Pass: built `desktop\build\bin\maddog-dev.exe` and packaged `dist\Maddog-windows-amd64-dev.zip`; launch skipped; bin dir not cleaned; Wails version mismatch warning was non-fatal. |

## Optional Visual Evidence

- External Chrome/opencli is now available according to `opencli doctor`, but this pass did not start a GUI server or capture screenshots.
- No Codex in-app browser was used.
- The GUI path is covered by desktop Go bridge tests, frontend contract tests, frontend production build, and the serve/browser-client HTTP smoke above.
- For screenshot evidence, capture Settings > Models/Providers, Settings/Skills offline replay entry, Capabilities skill candidates, transcript/status provider events, context compression, and code intelligence benchmark areas.
