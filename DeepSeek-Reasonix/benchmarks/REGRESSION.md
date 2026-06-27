# Maddog Benchmark Regression

This directory contains the repeatable checks used to validate Maddog-specific mechanisms and general coding-agent behavior.

## Layers

| Layer | Command | What it proves |
|---|---|---|
| Unified regression | `powershell -ExecutionPolicy Bypass -File scripts/run-maddog-regression.ps1` | Offline required regression across Maddog mechanism Go tests, C2 eval/replay/promote, desktop Go tests, frontend checks, CLI build, and e2e manifest generation. Writes `.benchmark/regression/latest.json` and `.benchmark/regression/latest.md`. |
| Unit tests | `go test ./cmd/e2ebench ./internal/cli ./internal/agent ./internal/boot ./internal/config ./internal/control ./internal/eval ./internal/evidence ./internal/event ./internal/provider ./internal/provider/openai ./internal/provider/anthropic ./internal/provider/costwrap ./internal/skill ./internal/serve -count=1` | Benchmark metadata, provider/auth/frontier routing, advisor events, dynamic skills, readiness evidence, tinyctx/compaction plumbing, C2 replay/scorer/guardrail/promote, and runtime metrics serialization. |
| Coverage audit | `go test ./cmd/e2ebench -run TestMaddogBenchmarkCoverageAudit -count=1` | Fails if committed benchmark evidence drops coverage for provider/auth/frontier/small model, official OpenAI/Anthropic auth config, iCodeEasy API-key routing, advisor, tinyctx/compaction, C2, desktop parity, Maddog naming isolation, or external benchmark wiring. |
| Manifest | `go run ./cmd/e2ebench -mode manifest -out benchmarks/e2e/manifest.md -json benchmarks/e2e/manifest.json` | Every committed e2e task has prompt, tags, verifier, requirements, and mechanism coverage metadata. |
| Maddog e2e | `go run ./cmd/e2ebench -bin ./bin/maddog.exe -out benchmarks/e2e/latest.md -json benchmarks/e2e/latest.json` | Real-provider validation of Maddog mechanisms: provider config isolation, frontier/auth profile, project skills, readiness gate, compaction, and delegation. |
| External harness | `powershell -ExecutionPolicy Bypass -File scripts/run-coding-agent-benchmark.ps1` | Compatibility with `usamadar/coding-agent-benchmark` and cross-agent coding task performance. |

The unified script defaults to deterministic local checks. Use `-IncludeE2E` for real-provider Maddog e2e, `-IncludeFrontierSmoke` for live frontier validation when provider credentials are present, and `-IncludeExternal` for `usamadar/coding-agent-benchmark`. Use `-UseProxy` to route downloads through `http://127.0.0.1:10809`.

On Windows, the external harness adapter sets `PYTHONUTF8=1`, isolates Maddog's OS config directory under `.benchmark/maddog-home`, uses relative Python/Jest test paths, and builds `bin/maddog.exe` before invoking the harness. The C/C++ tasks still require a GNU-like toolchain (`make`, `gcc`, `g++`) on `PATH`.

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
| Frontier/small-model profile visibility | `provider-auth-frontier-profile` | Tags `frontier`, `small-model`, `desktop-parity`; expects tool-call metrics. |
| Maddog config isolation from Reasonix names | `project-config-isolation` | Verifier checks Maddog config wins over legacy `reasonix.toml`. |
| Runtime project skill invocation | `project-skill-invocation` | Uses `.maddog/skills/bench-summarizer/SKILL.md`; verifier checks generated output. |
| Readiness evidence gate | `readiness-evidence-gate` | Requires readiness metrics and project host checks. |
| Tinyctx/context/compaction behavior | `compaction` | Exercises long sequential reads and compacted context reporting; tagged `tinyctx` so focused coverage filters can select it. |
| Subagent delegation and session isolation | `subagent-delegation` | Exercises task delegation and output isolation. |
| C2 offline replay/scoring/promotion | Go package `internal/eval` | Unit tests capture replay bundles, run replays through a subagent session, parse/fallback frontier scores, enforce guardrails, and promote skills with `SkillPromoted` events. |
| Desktop GUI configuration and event display | Desktop Go + frontend checks in `run-maddog-regression.ps1` | Validates desktop settings/event wiring and TypeScript/CSS/frontend tests/build for provider/frontier/advisor display paths. |

## External Benchmark Notes

`usamadar/coding-agent-benchmark` is useful for coding ability and speed comparison, but it does not inspect Maddog mechanism events by itself. Treat it as a second-layer harness. Maddog mechanism regression remains owned by `cmd/e2ebench`, whose JSON output includes metrics such as `upgrade_events`, `advisor_events`, `skill_generated_events`, `tool_calls`, `tool_errors`, `tool_truncations`, and readiness counters. The coverage audit keeps the external harness optional and prevents it from being mistaken for proof of Maddog-specific mechanisms.
