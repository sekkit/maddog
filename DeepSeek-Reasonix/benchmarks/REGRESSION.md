# Maddog Benchmark Regression

This directory contains the repeatable checks used to validate Maddog-specific mechanisms and general coding-agent behavior.

## Layers

| Layer | Command | What it proves |
|---|---|---|
| Unified regression | `powershell -ExecutionPolicy Bypass -File scripts/run-maddog-regression.ps1` | Offline required regression across Maddog mechanism Go tests, the explicit coverage audit, all Go packages, C2 eval/replay/promote, all desktop Go module tests, frontend checks, CLI build, and e2e manifest generation. Writes `.benchmark/regression/latest.json` and `.benchmark/regression/latest.md`. |
| Unit tests | `go test ./cmd/e2ebench ./internal/cli ./internal/agent ./internal/boot ./internal/config ./internal/control ./internal/eval ./internal/evidence ./internal/event ./internal/provider ./internal/provider/openai ./internal/provider/anthropic ./internal/provider/costwrap ./internal/skill ./internal/serve -count=1` | Benchmark metadata, provider/auth/frontier routing, advisor events, dynamic skills, readiness evidence, tinyctx/compaction plumbing, C2 replay/scorer/guardrail/promote, and runtime metrics serialization. |
| Coverage audit | `go test ./cmd/e2ebench -run TestMaddogBenchmarkCoverageAudit -count=1` | Fails if committed benchmark evidence drops coverage for provider/auth/frontier/small model, official OpenAI/Anthropic auth config, iCodeEasy API-key routing, advisor, tinyctx/compaction, C2, desktop parity, Maddog naming isolation, or external benchmark wiring. |
| All Go packages | `go test ./... -count=1` | Guards every Go package with tests, including bots, built-in MCP, checkpointing, codegraph, hooks, LSP, memory, permissions, plugins, and tools beyond the focused mechanism package list. |
| Desktop Go module | `go test ./... -count=1` from `desktop/` | Guards the desktop app root plus release signing and updater subpackages in the nested Wails module. |
| Desktop frontend contracts | `npm run test:all` from `desktop/frontend/` | Type-checks frontend tests and runs GUI contracts for Maddog naming isolation, provider model refresh, small/background model controls, frontier route controls, provider auth controls, and advisor/runtime event presentation. |
| Manifest | `go run ./cmd/e2ebench -mode manifest -out benchmarks/e2e/manifest.md -json benchmarks/e2e/manifest.json` | Every committed e2e task has prompt, tags, verifier, requirements, and mechanism coverage metadata. |
| Local provider e2e | `go run ./cmd/e2ebench -mode local-fixture -bin ./bin/maddog.exe -out .benchmark/regression/local-provider.md -json .benchmark/regression/local-provider.json` | Required offline proof that Maddog's headless CLI can load project config, call OpenAI-compatible and Anthropic-native SSE providers, exercise official OpenAI bearer and Anthropic workload identity auth paths, receive streamed `tool_calls` / `tool_use`, execute the real `write_file` tool, send tool results back on the next provider request, upgrade from small model to frontier after repeated tool failures, and persist run metrics. |
| Live official auth smoke | `go run ./cmd/e2ebench -mode official-auth-smoke -out .benchmark/regression/official-auth.md -json .benchmark/regression/official-auth.json` | Live provider proof that `OPENAI_OFFICIAL_TOKEN` is used as an official OpenAI bearer token and `ANTHROPIC_IDENTITY_TOKEN` is exchanged through Anthropic workload identity before calling `/v1/messages`. |
| Maddog e2e | `go run ./cmd/e2ebench -bin ./bin/maddog.exe -out benchmarks/e2e/latest.md -json benchmarks/e2e/latest.json` | Real-provider validation of Maddog mechanisms: provider config isolation, frontier/auth profile, project skills, readiness gate, compaction, and delegation. |
| External harness | `powershell -ExecutionPolicy Bypass -File scripts/run-coding-agent-benchmark.ps1 -LocalSmoke` | Offline compatibility with `usamadar/coding-agent-benchmark`: starts a local OpenAI-compatible SSE fixture, invokes Maddog through the external harness, fixes the benchmark smoke task, and records Maddog run metrics. |

The unified script defaults to deterministic local checks. It writes a Live Readiness section to `.benchmark/regression/latest.md` and `live_readiness` to `.benchmark/regression/latest.json`, showing whether provider API-key e2e, official auth e2e, and frontier smoke can run with the credentials currently set. It also writes `completion_audit`, which marks offline-verified capabilities separately from live-provider-pending capabilities and lists any failed required regression steps. Use `-RequireComplete` for final acceptance: it fails when `completion_audit.complete` is false, even if the offline required steps passed. Use `-AuditOnly` to inspect the latest saved completion audit without rerunning the suite; combine it with `-RequireComplete` for a fast acceptance gate. Use `-IncludeE2E` for real-provider Maddog e2e, `-IncludeOfficialAuthSmoke` for live official OpenAI bearer plus Anthropic workload identity validation, `-IncludeFrontierSmoke` for live frontier validation when provider credentials are present, and `-IncludeExternal` for the offline local-smoke `usamadar/coding-agent-benchmark` adapter check. Use `-UseProxy` to route downloads through `http://127.0.0.1:10809`.

On Windows, the external harness adapter sets `PYTHONUTF8=1`, isolates Maddog's OS config directory under `.benchmark/maddog-home`, uses relative Python/Jest test paths, and builds `bin/maddog.exe` before invoking the harness. `-LocalSmoke` additionally builds `cmd/coding-agent-benchmark-fixture`, writes a temporary benchmark-local `maddog.toml`, and proves the external harness can drive a real Maddog provider/tool loop without live credentials. The full C/C++ tasks still require a GNU-like toolchain (`make`, `gcc`, `g++`) on `PATH`.

Latest local dry-run status on Windows: Python and TypeScript tasks execute and report parsed test counts after the adapter installs `pytest`; C/C++ tasks report an environment error until GNU `make` is installed.

Latest local Maddog e2e smoke: `fizzbuzz` plus `project-config-isolation` passed with DeepSeek default provider and produced metrics. Frontier upgrade was not exercised because this machine has no `ICODEEASY_API_KEY`.

## Task Selection

`e2ebench` supports focused runs before the full suite:

```powershell
go run ./cmd/e2ebench -mode manifest -tags frontier,skill
go run ./cmd/e2ebench -bin ./bin/maddog.exe -tasks project-config-isolation,provider-auth-frontier-profile
```

## Current Mechanism Coverage

| Maddog capability | Primary e2e task | Evidence |
|---|---|---|
| OpenAI-compatible provider, iCodeEasy API-key routing, and official auth config | `provider-auth-frontier-profile` | Writes and verifies `auth-frontier-profile.json` from `maddog.toml`, including API-key, bearer, workload identity, official OpenAI, official Anthropic, and desktop provider access metadata without leaking token values. |
| Frontier/small-model profile visibility plus upgrade/advisor config | `provider-auth-frontier-profile` | Tags `frontier`, `small-model`, `upgrade`, `advisor`, `desktop-parity`; verifier checks upgrade budget/threshold and advisor guardrail settings. |
| Maddog config isolation from Reasonix names | `project-config-isolation` | Verifier checks Maddog config wins over legacy `reasonix.toml`. |
| Runtime project skill invocation | `project-skill-invocation` | Uses `.maddog/skills/bench-summarizer/SKILL.md`; verifier checks generated output. |
| Offline OpenAI-compatible provider/tool loop and metrics | `local-provider-tool-loop` via `-mode local-fixture` | Starts an in-process OpenAI-compatible SSE fixture, writes project `maddog.toml` to point Maddog at it, verifies `fixture-output.txt`, `.run-metrics.json`, `tool_calls`, zero `tool_errors`, and at least two provider steps. |
| Offline Anthropic frontier provider/tool loop and metrics | `local-anthropic-tool-loop` via `-mode local-fixture` | Starts an in-process Anthropic Messages SSE fixture, verifies `tool_use` / `tool_result` pairing, `anthropic-fixture-output.txt`, `.run-metrics.json`, zero `tool_errors`, and at least two provider steps. |
| Offline small-model to frontier upgrade routing | `local-frontier-upgrade` via `-mode local-fixture` | Starts one OpenAI-compatible fixture exposing small and frontier models; the small model emits three failing tool calls, Maddog upgrades to the frontier model, and the verifier checks `upgrade_events`, `tool_errors`, and at least four provider steps. |
| Offline official auth provider paths | `local-official-auth` via `-mode local-fixture` | Verifies OpenAI bearer `Authorization` header, Anthropic workload identity `/v1/oauth/token` exchange, minted Anthropic bearer token usage, and upgrade metrics through a real `maddog run`. |
| Live official OpenAI/Anthropic auth | `official-auth-smoke` via `-IncludeOfficialAuthSmoke` | Requires `OPENAI_OFFICIAL_TOKEN` and `ANTHROPIC_IDENTITY_TOKEN`; forwards optional Anthropic workload identity metadata from `ANTHROPIC_FEDERATION_RULE_ID`, `ANTHROPIC_ORGANIZATION_ID`, `ANTHROPIC_SERVICE_ACCOUNT_ID`, and `ANTHROPIC_WORKSPACE_ID`; directly instantiates Maddog's OpenAI and Anthropic providers, drains a small stream from each, and writes `.benchmark/regression/official-auth.json`. |
| Readiness evidence gate | `readiness-evidence-gate` | Requires readiness metrics and project host checks. |
| Tinyctx/context/compaction behavior | `compaction` | Exercises long sequential reads and compacted context reporting; tagged `tinyctx` so focused coverage filters can select it. |
| Subagent delegation and session isolation | `subagent-delegation` | Exercises task delegation and output isolation. |
| C2 offline replay/scoring/promotion | Go package `internal/eval` | Unit tests capture replay bundles, run replays through a subagent session, parse/fallback frontier scores, enforce guardrails, and promote skills with `SkillPromoted` events. |
| Desktop GUI configuration, event display, signing, and updater | Desktop Go + frontend checks in `run-maddog-regression.ps1` | Validates desktop settings/event wiring, release signing manifest generation, updater helpers, and TypeScript/CSS/frontend tests/build for provider/frontier/advisor display paths. Frontend contract tests specifically pin the small/background model selector, frontier model/threshold/budget controls, OpenAI-compatible and Anthropic custom provider auth modes, background model refresh behavior, and advisor/runtime cards. |

## External Benchmark Notes

`usamadar/coding-agent-benchmark` is useful for coding ability and speed comparison, but it does not inspect Maddog mechanism events by itself. Treat it as a second-layer harness. Maddog mechanism regression remains owned by `cmd/e2ebench`, whose JSON output includes metrics such as `upgrade_events`, `advisor_events`, `skill_generated_events`, `tool_calls`, `tool_errors`, `tool_truncations`, and readiness counters. The `-LocalSmoke` path proves the external harness can invoke Maddog end to end without live credentials; full cross-agent performance runs still need real providers and the requested task/toolchain set. The coverage audit keeps the external harness optional and prevents it from being mistaken for proof of Maddog-specific mechanisms.
