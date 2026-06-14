# Migrating to Maddog 1.0 (the Go rewrite)

Maddog 1.0 is a **ground-up rewrite in Go**. It is a new codebase, not an
incremental upgrade of the `0.x` TypeScript releases. This guide explains what
changed and how to move over.

## TL;DR

| | Legacy (v1) | Maddog 1.0+ (v2) |
|---|---|---|
| Language | TypeScript / Node | Go |
| Branch | [`v1`](https://github.com/sekkit/maddog/tree/v1) (maintenance only) | `main-v2` (default, active) |
| Versions | `0.x` (up to v0.54.x) | `1.0.0`+ |
| Install | `npm i -g maddog` (the `latest` tag, stays on `0.x`) | `npm i -g maddog@next` — `latest` deliberately stays on `0.x`; or a release archive / `go build` |
| Code intelligence | embedding semantic search | bundled [CodeGraph](https://github.com/colbymchenry/codegraph) (symbol/call graph) |

"v1" and "v2" are **codebase generations**, not semver: the v1 line never reached
1.0, so the Go rewrite takes the `1.x` major.

## Installing 1.0

`npm` stays the primary channel — the package wraps the prebuilt Go binary (the
same way esbuild/biome ship native binaries via npm). The binary itself is a
standalone Go executable; npm is only the installer, not a runtime dependency.

**`npm i -g maddog` deliberately still installs `0.x`.** A bare install — and
`npx maddog`, and 0.53's own `update` — follows npm's `latest` tag, which we
keep pinned to the `0.x` line so existing users aren't pulled into the rewrite
without asking. v1.x (Go) ships under the `next` tag; opt in explicitly:

```sh
npm i -g maddog@next     # or pin a version: maddog@1.1.0
maddog chat
```

`latest` will stay on `0.x` for the foreseeable future, so installing or
updating v2 always means `@next` (or a pinned `1.x`).

Prebuilt archives (`maddog-<os>-<arch>.tar.gz` / `.zip`) and the desktop
installer are attached to each GitHub release. These are a **separate channel**
from npm: the installer drops a standalone desktop/binary build and does not
touch a CLI you installed with `npm i -g`, so the two coexist — an npm `0.53` in
your shell alongside a `1.x` desktop app is expected, not a conflict. Or build
from source:

```sh
git clone https://github.com/sekkit/maddog   # default: main-v2 (Go)
cd maddog && make build                        # -> bin/maddog(.exe)
```

## Configuration

| Legacy | Maddog 1.0 |
|---|---|
| `reasonix.toml` (project) / `~/.config/reasonix/config.toml` (user) | `maddog.toml` (project) / `~/.config/maddog/config.toml` (user) — see `maddog.example.toml` |
| `.env` or the environment (`DEEPSEEK_API_KEY`, `MIMO_API_KEY`, …) | `.env`, Maddog credentials, or the environment via `api_key_env` |
| `REASONIX.md` (+ auto-memory) | `MADDOG.md` (+ auto-memory), Claude-Code-compatible |
| Legacy MCP servers in `~/.reasonix/config.json` | `[[plugins]]` in `maddog.toml`, or a Claude-Code `.mcp.json` (read as-is) |

Maddog intentionally does **not** auto-read or auto-import `~/.reasonix`,
`~/.config/reasonix`, `.reasonix`, or `reasonix.toml`. This keeps an installed
DeepSeek Reasonix and Maddog from changing each other's configuration or runtime
state. To migrate, copy the values you still want into Maddog-owned files by
hand: `maddog.toml`, `~/.config/maddog/config.toml`, `.maddog/`, or `~/.maddog/`.

## What's the same

The agent core carries over: the loop, tools (read/write/edit/glob/grep/bash/…),
subagents (`task`, explore/research/review), skills, hooks, plan mode, MCP client,
and DeepSeek prefix-cache–oriented design.

## What's different

- **Code intelligence**: embedding semantic search is replaced by **CodeGraph**
  (`codegraph_*` tools) — a tree-sitter symbol/call graph, no embedding service or
  API cost. New (first-run) configs start with it off; existing configs keep it
  on across upgrades. Toggle `[codegraph]` in the MCP manager or config, and set
  `[codegraph].tier` to choose lazy, background, or eager startup.
- **Plan mode** + `complete_step` (evidence-backed step sign-off).
- **No web dashboard** — the v2 line is terminal + desktop (Wails), by design.
- Some granular v1 tools are intentionally consolidated (e.g. file-management ops
  go through `bash`); a few v1 tools are not yet ported (tracked on Discussions).

## File encoding

Maddog 1.0 supports reading and editing files in UTF-8, UTF-8 BOM, UTF-16
LE/BE, and GB18030 (a superset of GBK). This matches v1's behavior.

- `read_file` decodes any supported encoding to UTF-8 for the model.
- `edit_file` and `multi_edit` preserve the file's original encoding — if you
  edit a GB18030 file, it stays GB18030 on disk.
- `write_file` always writes UTF-8 (the model's output encoding).
- `grep` decodes before matching, so regex patterns work on non-UTF-8 files.

## Reporting issues

Issues and PRs are labelled by line: **`v1`** (legacy TypeScript) and **`v2`**
(Go). File new reports against the line you're using. The legacy `v1` line is in
maintenance mode — bug fixes only, no new features.

Questions? Open a [Discussion](https://github.com/sekkit/maddog/discussions).
