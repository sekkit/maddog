# Maddog Benchmark Regression

This directory contains the repeatable checks used to validate Maddog-specific mechanisms and general coding-agent behavior.

## Layers

| Layer | Command | What it proves |
|---|---|---|
| Unit tests | `go test ./cmd/e2ebench ./internal/cli -count=1` | Benchmark metadata, manifest validation, expectation gates, and runtime metrics serialization. |
| Manifest | `go run ./cmd/e2ebench -mode manifest -out benchmarks/e2e/manifest.md -json benchmarks/e2e/manifest.json` | Every committed e2e task has prompt, tags, verifier, requirements, and mechanism coverage metadata. |
| Maddog e2e | `go run ./cmd/e2ebench -bin ./bin/maddog.exe -out benchmarks/e2e/latest.md -json benchmarks/e2e/latest.json` | Real-provider validation of Maddog mechanisms: provider config isolation, frontier/auth profile, project skills, readiness gate, compaction, and delegation. |
| External harness | `powershell -ExecutionPolicy Bypass -File scripts/run-coding-agent-benchmark.ps1` | Compatibility with `usamadar/coding-agent-benchmark` and cross-agent coding task performance. |

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
| OpenAI-compatible provider and API-key routing | `provider-auth-frontier-profile` | Writes and verifies `auth-frontier-profile.json` from `maddog.toml`. |
| Frontier/small-model profile visibility | `provider-auth-frontier-profile` | Tags `frontier`, `small-model`, `desktop-parity`; expects tool-call metrics. |
| Maddog config isolation from Reasonix names | `project-config-isolation` | Verifier checks Maddog config wins over legacy `reasonix.toml`. |
| Runtime project skill invocation | `project-skill-invocation` | Uses `.maddog/skills/bench-summarizer/SKILL.md`; verifier checks generated output. |
| Readiness evidence gate | `readiness-evidence-gate` | Requires readiness metrics and project host checks. |
| Context/compaction behavior | `compaction` | Exercises long sequential reads and compacted context reporting. |
| Subagent delegation and session isolation | `subagent-delegation` | Exercises task delegation and output isolation. |

## External Benchmark Notes

`usamadar/coding-agent-benchmark` is useful for coding ability and speed comparison, but it does not inspect Maddog mechanism events by itself. Treat it as a second-layer harness. Maddog mechanism regression remains owned by `cmd/e2ebench`, whose JSON output includes metrics such as `upgrade_events`, `advisor_events`, `skill_generated_events`, `tool_calls`, `tool_errors`, `tool_truncations`, and readiness counters.
