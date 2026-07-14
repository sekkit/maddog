# Skill Optimization

Maddog includes an experimental, native Go implementation of the core
SkillOpt-Lite loop. It uses SkillOpt-Lite as an algorithm reference, but does
not vendor or invoke the upstream Python trainer. The implementation reviewed
for this integration was
[`EvolvingLMMs-Lab/SkillOpt-Lite`](https://github.com/EvolvingLMMs-Lab/SkillOpt-Lite)
at commit `32708a1349105b59183afb1b417385aa4e8812e0`.

Optimization is an explicit offline workflow. Normal Maddog turns do not train,
rewrite, or promote a skill. The engine creates immutable candidate revisions;
deployment is a separate, approval-gated command.

## Pipeline mapping

The upstream SkillOpt-Lite diagram can be read as six stages. Maddog maps them
to the following modules:

| Stage | Upstream purpose | Maddog implementation |
| --- | --- | --- |
| 1. Freeze inputs | Select train, validation, and test data; freeze the target agent; choose the trainable Markdown skill. | `internal/skillopt/manifest.go` validates a non-overlapping three-way dataset. `Engine.Start` snapshots the dataset, config, initial project skill, and their digests. |
| 2. Produce training trajectories | Run a sampled training batch with the current skill. | `internal/skillopt/engine.go` samples deterministically by seed. `EvalbenchExecutor` and `internal/evalbench` run each case in a disposable workspace through `maddog run --eval-mode --skill ...`. |
| 3. Explore and diagnose | Inspect trajectories, cluster failures, and select useful evidence within a context budget. | `ProviderProposer` receives bounded, redacted training inputs, outputs, scores, and verifier evidence. Unlike upstream's coding-agent file exploration, Maddog currently builds one structured proposal prompt. |
| 4. Edit the skill | Diagnose common patterns and apply a bounded update to the skill. | The proposer returns a complete candidate body plus ordered `BodyEdit` byte ranges. `validateAndApplyProposal` replays the patch, enforces edit/size budgets, and rejects changes to name, description, tools, run mode, model, effort, scope, or path. |
| 5. Validate and gate | Compare on independent validation data; accept improvements and retain rejection history. | Every round evaluates baseline, current, and candidate revisions on the same validation cases and seeds. `StrictGate` is lexicographic: hard verifier rate cannot regress; when hard rate ties, soft score must clear the deadband and `min_delta`. |
| 6. Finalize and deploy | Evaluate the best skill, retain history, and publish only an accepted artifact. | The engine evaluates only `BestRevisionID` on the held-out test split, then marks the run complete. `JSONRunStore` retains revisions, proposals, decisions, usage, and checkpoints. `PromoteBest` and `RollbackPromotion` use content-hash compare-and-swap and an exact byte snapshot. |

The frozen target is Maddog's agent runtime and tool harness. This integration
implements skill optimization only. It does not implement upstream HarnessOpt,
which also edits agent or tool code.

## Project configuration

Optimization must be enabled in the project's `maddog.toml`:

```toml
[skills.optimization]
enabled = true

# Optional normal-turn replay capture. This is not required by an explicit
# manifest/suite optimization run and defaults to false.
capture_replay = false

# Disabled by default. When true, evaluation exposes a no-network,
# secret-scrubbed OS-sandboxed shell. See the security caveat below.
allow_shell = false

model = "target-model"
proposer_model = "optimizer-model"
rounds = 3
batch_size = 4
max_concurrency = 1
min_delta = 0.01
deadband = 0.001

# Zero means no limit for that dimension.
max_calls = 80
max_input_tokens = 120000
max_output_tokens = 20000
max_cost = 5.00

retention_days = 30
max_replay_bundles = 200
redact_artifacts = true
require_approval = true
```

The conservative defaults are `enabled = false`, `capture_replay = false`,
artifact redaction on, promotion approval on, three rounds, a batch of four,
and one worker. Model names must resolve through Maddog's normal provider/model
configuration.

## Dataset manifest

`--manifest` accepts JSON or TOML. The manifest is immutable for the run and
must contain non-empty `train`, `validation`, and `test` splits. Case IDs must
be globally unique across all splits. Every case needs a non-empty input.

```toml
id = "parser-skill-v1"

[[train]]
id = "parser-train-001"
input = "Repair the malformed CSV parser in this workspace."
expected = "all parser tests pass"
[train.metadata]
task_id = "parser-train-001"

[[train]]
id = "parser-train-002"
input = "Add quoted-field handling without changing the public API."
[train.metadata]
task_id = "parser-train-002"

[[validation]]
id = "parser-val-001"
input = "Fix escaped delimiters and preserve existing behavior."
[validation.metadata]
task_id = "parser-val-001"

[[test]]
id = "parser-test-001"
input = "Repair multiline quoted records in the provided fixture."
[test.metadata]
task_id = "parser-test-001"
```

The equivalent JSON shape is:

```json
{
  "id": "parser-skill-v1",
  "train": [
    {
      "id": "parser-train-001",
      "input": "Repair the malformed CSV parser in this workspace.",
      "expected": "all parser tests pass",
      "metadata": { "task_id": "parser-train-001" }
    }
  ],
  "validation": [
    {
      "id": "parser-val-001",
      "input": "Fix escaped delimiters and preserve existing behavior.",
      "metadata": { "task_id": "parser-val-001" }
    }
  ],
  "test": [
    {
      "id": "parser-test-001",
      "input": "Repair multiline quoted records in the provided fixture.",
      "metadata": { "task_id": "parser-test-001" }
    }
  ]
}
```

`expected` is optional JSON and is exposed to the optimizer only for sampled
training cases. Validation and test correctness should live in hidden,
deterministic verifiers, not in prompts or model-only judgments.

`metadata.task_id` selects a task from the evalbench suite. When omitted, the
case ID is used as the task ID.

## Evalbench suite

The suite passed with `--suite` has this layout:

```text
parser-suite/
  tasks/
    parser-train-001/
      task.toml
      workdir/
      verify.py
    parser-train-002/
      task.toml
      workdir/
      verify.sh
    parser-val-001/
      task.toml
      workdir/
      verify.py
    parser-test-001/
      task.toml
      workdir/
      verify.py
```

Example `task.toml`:

```toml
prompt = "This value is replaced by the manifest case input at runtime."
max_steps = 12
timeout_sec = 240
tags = ["parser", "local"]
```

`workdir/` is copied into a fresh temporary directory for each rollout. The
agent cannot see `verify.py` or `verify.sh`: evalbench copies the verifier into
the workspace only after the agent process exits. A successful verifier
defaults to `hard = 1` and `soft = 1`. A verifier can instead write
`skillopt-score.json` (or `.skillopt-score.json`):

```json
{ "hard": 1.0, "soft": 0.85 }
```

Both scores are clamped to `[0, 1]`. The verifier is trusted local code and
must return a non-zero exit status when the case is invalid.

## CLI workflow

Run commands from the project root. The skill must already exist under a
project convention root such as `.maddog/skills/<name>/SKILL.md`.

```sh
maddog skillopt optimize \
  --skill parser-helper \
  --manifest benchmarks/parser/dataset.toml \
  --suite benchmarks/parser \
  --run-id parser-helper-20260713 \
  --json
```

Use `--binary <path>` when testing a locally built Maddog executable. Useful
per-run overrides include `--model`, `--proposer-model`, `--rounds`,
`--batch-size`, `--max-calls`, `--max-input-tokens`, `--max-output-tokens`, and
`--max-cost`. `--keep-workspaces` retains case workspaces for diagnosis and
should be used only with non-sensitive fixtures.

Inspect, cancel, or resume a durable run:

```sh
maddog skillopt status --run parser-helper-20260713 --json
maddog skillopt cancel --run parser-helper-20260713
maddog skillopt resume --run parser-helper-20260713 --suite benchmarks/parser
```

Optimization completion does not alter the active skill. Review status and
then promote explicitly:

```sh
maddog skillopt promote --run parser-helper-20260713 --yes --json
```

Rollback restores the exact pre-promotion bytes and refuses to overwrite a
post-promotion user edit:

```sh
maddog skillopt rollback \
  --run parser-helper-20260713 \
  --yes \
  --reason "held-out regression"
```

Remove old terminal checkpoints. Active, paused, and still-promoted runs are
retained so they remain resumable or rollback-capable:

```sh
maddog skillopt cleanup --older-than 720h
```

## Security boundaries

- The workflow is opt-in twice: project configuration enables optimization,
  and a user must invoke `optimize` or `resume`. The engine never promotes.
- Rollouts use disposable workspaces and `--eval-mode`. Evaluation loads only
  project skills, suppresses session/archive/user-memory persistence, disables
  hooks, plugins/MCP, advisor/upgrade paths, runtime skill orchestration, and
  replay capture.
- Built-in readers and writers are confined to the disposable workspace.
  Shell execution is disabled by default. With `allow_shell = true`, the shell
  is exposed only when Maddog can enforce its OS sandbox; its network is denied
  and secret-bearing environment variables are scrubbed. The configured model
  provider still requires its normal API connectivity.
- Hidden verifiers are unavailable during rollout and execute only afterward.
  Verifiers and seed fixtures are trusted local inputs; a malicious verifier
  can execute with the user's host permissions.
- Train, validation, and test IDs cannot overlap. The proposer receives only
  sampled training cases. Validation uses paired baseline/current/candidate
  rollouts on identical case IDs and seeds; test runs only on the selected best
  revision after all rounds.
- Only the Markdown body is trainable. Structured edits, UTF-8 boundaries,
  changed-byte limits, body-size limits, and frozen capability metadata are
  validated locally rather than trusted to model output.
- Every rollout and proposal contributes to call, token, and estimated-cost
  budgets. Cancellation is sticky in the checkpoint store, and completed work
  is reused when a paused run resumes.
- Stored rollout output and structured evidence are redacted for common
  credentials and image data URLs. Normal-turn replay capture is separately
  disabled by default and has age/count retention controls.
- Promotion requires completed held-out test evidence. With the default config
  it also requires `--yes`. Promotion and rollback use content hashes and an
  immutable snapshot, so concurrent edits fail instead of being overwritten.

Data still leaves the machine when sent to the configured rollout or proposer
model. Do not put secrets in manifests, prompts, fixtures, or `maddog.toml`.
The project config is copied into disposable workspaces; provider credentials
should be environment references rather than literal values.

## Current limits

- This is skill-body optimization, not HarnessOpt. Agent code, tool code,
  frontmatter, and allowed tools are frozen.
- The upstream coding-agent directory exploration step is reduced to one
  bounded structured proposer prompt. There is no trajectory browser,
  reflection pool, slow-update policy, or historical rejection buffer.
- Rollouts are currently executed sequentially. `max_concurrency` is retained
  in configuration/checkpoints but does not yet parallelize the engine.
- Statistical significance tests and repeated stochastic trials are not yet
  implemented. Deterministic fixtures, stable seeds, sufficient validation
  coverage, and a meaningful `min_delta` remain the suite author's
  responsibility.
- Evalbench currently supports Python verifiers and a portable subset of shell
  verifier patterns when Bash is unavailable.
- An opt-in evaluation shell retains the host-read behavior of Maddog's current
  OS sandbox, apart from configured `forbid_read` roots. Use it only with trusted
  tasks and a suitably isolated host/container; the default is `allow_shell =
  false`.
- The optimizer requires an existing project skill and local evalbench suite.
  It does not synthesize a new skill, download benchmark data, or auto-promote
  a result.
- Checkpoints are local JSON files under `.maddog/skillopt/runs` by default.
  There is no distributed coordinator or remote artifact store.
