## Maddog e2e benchmark manifest

**Status:** valid · **Tasks:** 11 · **Tags:** 36 · **Requirements:** 11

| Task | Tags | Requires | Expectations | Max steps | Timeout | Seed | Verify | Issues |
|---|---|---|---|---:|---:|---|---|---|
| `compaction` | `context`, `sequential-read`, `tool-output`, `compaction`, `tinyctx`, `headless-cli` | `provider`, `filesystem`, `markdown` | — | 40 | 900 | yes | yes | — |
| `fix-add-bug` | `core`, `python`, `bugfix`, `edit-existing-file`, `headless-cli` | `provider`, `filesystem`, `python3` | — | 12 | 180 | yes | yes | — |
| `fizzbuzz` | `core`, `python`, `scratch`, `file-write`, `headless-cli` | `provider`, `filesystem`, `python3` | — | 12 | 180 | no | yes | — |
| `local-anthropic-tool-loop` | `local-fixture`, `anthropic`, `frontier`, `provider`, `tool-loop`, `metrics`, `headless-cli` | `local-anthropic-fixture`, `filesystem` | tool calls >= 1; tool errors <= 0 | 4 | 90 | no | yes | — |
| `local-provider-tool-loop` | `local-fixture`, `provider`, `tool-loop`, `metrics`, `headless-cli` | `local-openai-fixture`, `filesystem` | tool calls >= 1; tool errors <= 0 | 4 | 90 | no | yes | — |
| `palindrome` | `core`, `python`, `scratch`, `file-write`, `headless-cli` | `provider`, `filesystem`, `python3` | — | 12 | 180 | no | yes | — |
| `project-config-isolation` | `config`, `maddog-isolation`, `provider`, `frontier`, `headless-cli` | `provider`, `filesystem`, `maddog.toml` | tool calls >= 2 | 18 | 240 | yes | yes | — |
| `project-skill-invocation` | `skill`, `project-skill`, `run-skill`, `headless-cli` | `provider`, `filesystem`, `skills` | tool calls >= 2 | 24 | 300 | yes | yes | — |
| `provider-auth-frontier-profile` | `provider`, `auth`, `frontier`, `small-model`, `advisor`, `upgrade`, `desktop-parity`, `headless-cli` | `provider`, `filesystem`, `maddog.toml` | tool calls >= 2 | 24 | 300 | yes | yes | — |
| `readiness-evidence-gate` | `evidence`, `readiness`, `host-checks`, `verification`, `headless-cli` | `provider`, `filesystem`, `bash`, `project-memory` | readiness checks >= 1; tool calls >= 2; tool errors <= 0 | 24 | 300 | yes | yes | — |
| `subagent-delegation` | `subagent`, `delegation`, `tool-policy`, `session-isolation`, `headless-cli` | `provider`, `filesystem`, `task-tool` | — | 15 | 300 | yes | yes | — |

### Tag Coverage

- `advisor`: 1 task(s)
- `anthropic`: 1 task(s)
- `auth`: 1 task(s)
- `bugfix`: 1 task(s)
- `compaction`: 1 task(s)
- `config`: 1 task(s)
- `context`: 1 task(s)
- `core`: 3 task(s)
- `delegation`: 1 task(s)
- `desktop-parity`: 1 task(s)
- `edit-existing-file`: 1 task(s)
- `evidence`: 1 task(s)
- `file-write`: 2 task(s)
- `frontier`: 3 task(s)
- `headless-cli`: 11 task(s)
- `host-checks`: 1 task(s)
- `local-fixture`: 2 task(s)
- `maddog-isolation`: 1 task(s)
- `metrics`: 2 task(s)
- `project-skill`: 1 task(s)
- `provider`: 4 task(s)
- `python`: 3 task(s)
- `readiness`: 1 task(s)
- `run-skill`: 1 task(s)
- `scratch`: 2 task(s)
- `sequential-read`: 1 task(s)
- `session-isolation`: 1 task(s)
- `skill`: 1 task(s)
- `small-model`: 1 task(s)
- `subagent`: 1 task(s)
- `tinyctx`: 1 task(s)
- `tool-loop`: 2 task(s)
- `tool-output`: 1 task(s)
- `tool-policy`: 1 task(s)
- `upgrade`: 1 task(s)
- `verification`: 1 task(s)

### Requirement Coverage

- `bash`: `readiness-evidence-gate`
- `filesystem`: `compaction`, `fix-add-bug`, `fizzbuzz`, `local-anthropic-tool-loop`, `local-provider-tool-loop`, `palindrome`, `project-config-isolation`, `project-skill-invocation`, `provider-auth-frontier-profile`, `readiness-evidence-gate`, `subagent-delegation`
- `local-anthropic-fixture`: `local-anthropic-tool-loop`
- `local-openai-fixture`: `local-provider-tool-loop`
- `maddog.toml`: `project-config-isolation`, `provider-auth-frontier-profile`
- `markdown`: `compaction`
- `project-memory`: `readiness-evidence-gate`
- `provider`: `compaction`, `fix-add-bug`, `fizzbuzz`, `palindrome`, `project-config-isolation`, `project-skill-invocation`, `provider-auth-frontier-profile`, `readiness-evidence-gate`, `subagent-delegation`
- `python3`: `fix-add-bug`, `fizzbuzz`, `palindrome`
- `skills`: `project-skill-invocation`
- `task-tool`: `subagent-delegation`
