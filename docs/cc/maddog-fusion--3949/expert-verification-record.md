---
slugid: maddog-fusion--3949
stage: expert-verification
date: 2026-07-03
source_matrix: docs/cc/maddog-fusion--3949/verification-matrix.md
---

# Maddog Fusion Expert Verification Record

This file coordinates the expert-agent verification pass against `verification-matrix.md`.
It records which feature groups were reviewed, what evidence was checked, what gaps were found,
and what code or tests were changed to align implementation with the design goal.

## Coordination Rules

- Repository root: `C:\Dev2\maddog`.
- Do not use the Codex in-app browser; GUI smoke, if needed, must use external Chrome/opencli.
- Preserve unrelated existing worktree changes.
- Treat tests and search results as evidence only when they directly cover the requirement.
- Confirmed missing or misunderstood behavior must be fixed in code, then re-verified.

## Expert Assignments

| Expert | Agent ID | Scope | Status | Output |
| --- | --- | --- | --- | --- |
| Expert A | `019f2721-fea7-7d21-aba9-005d663c4025` | Multi-provider acceptance; failure signal/frontier upgrade; advisor skill/runtime events | Complete | Final pass fulfilled after built-in advisor skill, slash exposure, runtime events, and advisor runner gate fixes |
| Expert B | `019f2722-56f9-7560-ab06-0f249a5dc978` | Runtime skill orchestration; offline replay self-improvement; rule/LLM hybrid review | Complete | Final pass fulfilled after runtime replay capture, held-out CLI guardrail, aggregate persistence, and GUI audit/action coverage |
| Expert C | `019f2722-a72e-7141-a6e1-2ab167ff734d` | Provider profile/auth/budget/status; context compression/raw lookup; code intelligence backend/benchmark | Partial | Provider/profile and context compression are fulfilled; code intelligence benchmark now distinguishes real selected backends from external-contract or degraded adapters, and local smoke is no longer counted as real completion |
| Expert D | `019f2722-f8ca-7752-a53f-d4a30e90ab5d` | Desktop GUI projection; offline replay GUI; sidebar logo/wordmark; cross-stage integration | Complete | Final pass fulfilled except optional screenshot artifact; command-palette shortcut, audit reader, brand visibility, tests/build, and package build pass |
| Expert G | `019f276b-5640-7db1-80f3-02abf72b3ff1` | Backend, CLI, config, and skilleval follow-up | Complete | Found held-out validation and advisor runner gate gaps; fixes applied and re-tested |
| Expert H | `019f276b-9cdb-7371-9a0e-89946fb0a2c8` | GUI Settings/Capabilities verification | Complete | Found provider role UI missing despite backend data; role chips added and contract-tested |
| Expert I | `019f276b-d680-7d53-9d65-70b9268a420b` | Documentation, dependency readiness, acceptance metrics | Complete | Found matrix lacked task-level metrics and dependency checks; matrix updated with thresholds, artifacts, and evidence |

## Current Repository Snapshot

- Branch: `main`.
- Remote state at pass start: `main...origin/main`.
- Recent pushed commits include:
  - `d4aa9c1cd fix(desktop): restore sidebar wordmark`
  - `7562afe95 feat(desktop): surface offline replay skill evaluation`
  - `44e555c81 fix(desktop): repair skills settings and chrome spacing`
- Worktree note: many pre-existing modified files and research directories are present. This pass must not revert unrelated changes.

## Progress Log

| Time | Actor | Action | Result |
| --- | --- | --- | --- |
| 2026-07-03T16:40:52+08:00 | Coordinator | Started expert verification pass from `verification-matrix.md`. | Four read-only expert agents dispatched across independent feature surfaces. |
| 2026-07-03T16:47:00+08:00 | Expert B | Reviewed runtime skill orchestration, offline replay self-improvement, and rule/LLM hybrid review. | Found Offline replay self-improvement partial: runtime replay capture is not wired into controller/agent flow, CLI eval does not persist candidate evaluation/status, and desktop evaluation uses a weakened `MinBundles: 1` guardrail. |
| 2026-07-03T16:52:00+08:00 | Expert C | Reviewed provider/auth/budget/status, context compression/raw lookup, and code intelligence backend/benchmark. | Found context compression fulfilled; provider profile partial because advisor is not an explicit role; code-intelligence benchmark partial because desktop action uses a local-file surrogate instead of the selected backend. |
| 2026-07-03T16:55:00+08:00 | Expert A | Reviewed multi-provider acceptance, frontier upgrade, and advisor runtime events. | Found multi-provider fulfilled; frontier/advisor partial because boot expects an `advisor` skill but no built-in exists, and `/advisor` has no command route. |
| 2026-07-03T16:58:00+08:00 | Expert D | Reviewed desktop GUI projection, offline replay GUI, sidebar brand, and cross-stage integration. | Found GUI projections mostly present; missing GUI audit reader, command-palette entry for offline replay, collapsed sidebar brand visibility, and external visual smoke evidence. |
| 2026-07-03T18:00:00+08:00 | Expert G | Re-checked CLI/config/boot against the matrix after earlier fixes. | Required true held-out replay validation and advisor runner availability independent of frontier provider. |
| 2026-07-03T18:05:00+08:00 | Expert H | Re-checked GUI surfaces. | Required provider role display in Settings because backend already emits `ProviderView.roles`. |
| 2026-07-03T18:10:00+08:00 | Expert I | Re-checked evidence documentation and dependency readiness. | Required real acceptance metrics, dependency checks, and closed verification evidence instead of pending notes. |
| 2026-07-03T18:19:42+08:00 | Coordinator | Applied follow-up fixes and ran regression checks. | Held-out validation, advisor gate, provider role UI, docs, full Go, desktop Go, frontend tests/build, opencli, context-mode, benchmark package checks, and package build pass. |

## Findings And Fixes

### Expert A Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Multi-provider acceptance | Fulfilled | Provider registry, OpenAI-compatible and Anthropic providers, boot model resolution, and transport/tool-call tests exist. | Run provider/boot tests during final verification. |
| Failure signal and frontier upgrade | Fulfilled | Evidence signals, threshold upgrade policy, frontier switching/fallback, budget events, built-in `advisor` skill, and advisor runner are available. The runner is no longer gated on `frontierProv != nil`. | Automatic advisor-before-frontier still triggers from the frontier upgrade path; manual advisor remains available through slash-skill routing. |
| Advisor skill and runtime events | Fulfilled | Built-in `advisor` skill, slash command exposure, advisor event kinds, CLI rendering, serve/SSE mapping, desktop wire/frontend rendering, and subagent model precedence tests exist. | Keep runtime event contract in frontend/serve tests. |

### Expert B Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Runtime skill orchestration | Fulfilled, with lifecycle caveat | `internal/skill` store/validator/orchestrator and controller integration are present; tests cover existing matches, dynamic generation, and high-risk rejection. | Decide whether "one-turn dynamic skill" means automatic cleanup or session-local temporary skill, then document or enforce it. |
| Offline replay self-improvement | Fulfilled, with aggregate-persistence scope | Runtime controller captures replay bundles; `skilleval` CLI and desktop GUI share held-out provider replay evaluation, default `MinBundles: 5`, source/duplicate rejection, aggregate score/guardrail persistence, and promotion-grade provenance. Dry-run records preview-only evidence and `Promote` rejects non-promotion-grade records. | Persisting per-bundle replay detail is not currently required; add it only if future audit requirements need bundle-level artifacts beyond CLI JSON output. |
| Rule/LLM hybrid review | Fulfilled | `internal/review` deterministic rules, prompt builder, CLI wiring, and built-in review skill exposure are present and tested. | Run targeted review tests during final verification. |

### Expert C Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Provider profile, auth, budget, status | Fulfilled | Provider auth modes, default/planner/frontier/small/advisor roles, `advisor_model`, budget/status events, Settings role chips, and StatusBar snapshots exist and are tested. | Continue to treat credential values as unavailable to frontend/UI tests; only env names and status should display. |
| Context compression and raw-result lookup | Fulfilled | `internal/contextpack`, agent compression/raw storage, controller raw lookup, Settings controls, and desktop expansion paths are wired and tested. | Run targeted context tests during final verification. |
| Code intelligence backend and benchmark | Partial: real selected backend routing added, external MCP pending | Backend registry/config/Capabilities and CLI benchmark exist. CLI mock is opt-in; desktop no longer uses a local-files surrogate for arbitrary backend ids. Built-in CodeGraph uses the MCP benchmark adapter, HyperGraphRAG uses the sidecar contract, and generic external MCP backend benchmarking still needs a mapped-tool adapter before it can be marked fulfilled. | Verify selected CodeGraph/HyperGraphRAG paths with real sidecars; keep generic MCP backends marked external-contract/degraded until adapter execution is implemented. |

### Expert D Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Cross-stage runtime integration | Fulfilled except screenshot artifact | CLI/GUI contract tests, full Go, desktop Go, frontend tests/build, serve/SSE smoke, dependency checks, and opencli readiness are logged. | Run external Chrome screenshot smoke when a GUI server is running and screenshot artifacts are needed. |
| Desktop provider/settings/status/context/code-intelligence projection | Fulfilled | Settings, StatusBar, context compression controls, Capabilities, and code intelligence surfaces exist. | Run frontend tests/build after edits. |
| Offline replay self-improvement GUI | Fulfilled | Candidate actions render; evaluation accepts a request with held-out bundle paths/model/dry-run mode; Capabilities displays promotion-grade provenance, provider, bundle count, recent audit records, and preview-only dry-run failures; command palette includes an Offline replay self-improvement shortcut to Settings > Skills. | Visual screenshot can be appended later. |
| Sidebar brand logo/wordmark | Fulfilled | Expanded states render logo and `Maddog`; collapsed state keeps the compact logo visible while hiding only the wordmark; theme-style mobile CSS no longer hides the collapsed rail. | Covered by `app-chrome-tabs.test.ts`. |

### Expert G Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Held-out skilleval | Fulfilled | `validateHeldOutBundles` rejects source bundle ID/path and duplicate bundle ID/path; tests cover source and duplicate rejections. | None for current guardrail scope. |
| Advisor runner gate | Fulfilled with documented trigger scope | Boot now creates `advisorRunner` whenever advisor uses are enabled; advisor model precedence remains in `subagentModelRef`. | Automatic advisor invocation still belongs to the frontier-upgrade route unless manually invoked through slash skill. |
| CLI persistence | Fulfilled at aggregate level | `--store-dir` records aggregate average score and guardrail result; CLI JSON includes per-bundle scores/replays for the invocation. | Add persisted per-bundle records only if audit requirements expand. |

### Expert H Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Provider roles in Settings | Fulfilled | `ProviderAccessGroup.roles` aggregates backend roles; provider cards render group role chips and per-profile roles; locales include default/planner/frontier/small/advisor labels. | Optional visual screenshot after starting GUI server. |
| GUI contract | Fulfilled | `maddog-mechanisms-contract.test.ts` now asserts role aggregation, role label rendering, advisor locale key, and role field in `ProviderView`. | Keep contract test updated if role names change. |

### Expert I Findings

| Area | Verdict | Evidence | Required Follow-up |
| --- | --- | --- | --- |
| Acceptance metrics | Fulfilled | `verification-matrix.md` now defines thresholds/artifacts per feature area, including replay `N>=5`, score `>=0.70`, frontier threshold `3`, advisor use default `1`, and compression fixture saving `>=50%`. | Keep matrix updated when product defaults change. |
| Dependency readiness | Fulfilled | Matrix records Go/Wails/Node/npm/pnpm, context-mode, opencli, and codeintelbench/e2ebench readiness. | Capture screenshot artifacts later if release evidence requires them. |

## Verification Evidence

| Date | Evidence | Result |
| --- | --- | --- |
| 2026-07-03 | `go test ./internal/cli ./internal/boot ./internal/config -run "SkillEval|SubagentModelRef|Advisor|RemoveProvider|Render" -count=1` | Pass |
| 2026-07-03 | In `desktop`: `go test . -run "SettingsProviderProfiles|SetAdvisorModel|ProviderProfiles" -count=1` | Pass |
| 2026-07-03 | In `desktop/frontend`: `pnpm exec tsx src\__tests__\maddog-mechanisms-contract.test.ts`; `pnpm exec tsx src\__tests__\app-chrome-tabs.test.ts`; `pnpm exec tsx src\__tests__\capabilities-skill-candidates.test.ts`; `pnpm exec tsc --noEmit -p tsconfig.test.json` | Pass |
| 2026-07-03 | `opencli doctor`; `ctx_doctor`; Go/Wails/Node/npm/pnpm version checks | Pass; opencli browser bridge connected; context-mode OK with optional Bun warning |
| 2026-07-03 | `go test ./internal/control -count=1 -timeout 240s`; `go test ./cmd/codeintelbench ./cmd/e2ebench -count=1 -timeout 240s` | Pass |
| 2026-07-03 | Full regression: `go test ./... -count=1 -timeout 240s`; in `desktop`, `go test . -count=1 -timeout 240s`; in `desktop/frontend`, `npm run test:all`; `npm run build` | Pass; Vite emitted non-fatal chunk/dynamic-import/plugin timing warnings |
| 2026-07-03 | Package build: `scripts\build-package-run-maddog.ps1 ... -NoLaunch -NoClean` from `C:\Dev2\maddog\maddog` | Pass; produced `desktop\build\bin\maddog-dev.exe` and `dist\Maddog-windows-amd64-dev.zip`; Wails CLI/go.mod version warning is non-fatal |
| 2026-07-03 | Real-gap focused verification: skilleval/control/cli, codeintel/e2e/codegraph, desktop candidate/codeintel, frontend `test:all`, frontend `build`, and PowerShell regression script parse check | Pass; GUI promotion-grade replay, preview-only dry-run, strict real gates, and frontend contracts verified |
