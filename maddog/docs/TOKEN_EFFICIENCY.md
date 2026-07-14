# Token Efficiency

Maddog uses one owner for each kind of token reduction. This keeps tool output
recoverable and prevents a lossy result from being compressed a second time.

## Default Path

`contextpack` is Maddog's built-in, deterministic equivalent of RTK for tool
output. It is enabled by default and runs after a tool returns, before that
result enters model context.

```toml
[agent.context_compression]
policy = "auto"       # off | auto | aggressive
threshold_bytes = 8192 # auto only
max_bytes = 4096       # target model-visible size
```

For ordinary `git`, `rg`, test, build, and repetitive log output, leave this
at its defaults. Maddog retains the original result when compression actually
saves context, emits a `raw://tool/<id>` reference, and sends only the compact
result to the model.

```text
raw tool output -> raw artifact -> contextpack -> model-visible tool result
```

`tool_result` reads a retained artifact on demand. A legacy id-only request
preserves its historical behavior and is subject to the normal 32 KiB head/tail
safety cap. To request a bounded page instead, explicitly provide `offset` or
`limit`; the response carries a `next_offset`. `limit` defaults to 12 KiB,
must be at least 4 bytes, and is capped at 16 KiB, so a paged raw read is not
silently truncated by the normal tool-output ceiling.

Pagination bounds the model-visible response only. The current session artifact
store may still load and cache the retained raw result before slicing it; a
range-reading store is a separate future optimization.

```json
{"id":"raw://tool/call-123","offset":12288,"limit":12288}
```

Raw artifacts exist only for results that `contextpack` successfully compressed.
They are session-scoped, not a general-purpose full-history index.

## One Transform Per Result

Use exactly one primary transform for a command result:

| Need | Route | Do not combine with |
| --- | --- | --- |
| Routine CLI output | Raw command -> Maddog `contextpack` | External RTK for the same command |
| A quick, intentionally lossy terminal summary | Raw command -> external RTK -> model | Maddog `contextpack` or later raw retrieval |
| Persistent search of independently sourced files or web material | Raw source -> `context-mode` index -> bounded search hits | An RTK-filtered copy of that source |

Never feed RTK-filtered output into `context-mode`: removed details cannot be
recalled. Do not configure an external RTK wrapper and `contextpack` for the
same shell output. Maddog currently has no provenance field declaring that an
external tool already transformed a result. This is a manual operational rule,
not an enforceable per-command guard: an `rtk ...` command under `policy =
"auto"` can still be compressed again.

## External RTK

RTK is optional. Maddog does not intercept every shell command through an
external RTK proxy because that would hide the raw result from Maddog's session
store and duplicate the built-in deterministic compressor.

When a workflow deliberately invokes RTK itself, make it the sole output
transformer for that entire configured session or project:

```toml
[agent.context_compression]
policy = "off"
```

This trade-off is intentional: the model sees RTK's compact result, but Maddog
cannot offer the original through `tool_result`. Current configuration has no
per-command switch; restore `policy = "auto"` for normal Maddog-managed command
output.

## Optional context-mode MCP

`context-mode` is useful when the source is large, its size is unknown, or it
needs later search. It is an external MCP service with its own index and
lifecycle, so it is opt-in rather than a Maddog core dependency.

An explicit, pinned MCP configuration can look like this:

```toml
[[plugins]]
name = "context-mode"
command = "npx"
args = ["--yes", "context-mode@1.0.169"]
```

This launches a third-party executable MCP server, not a search-only adapter.
Its official surface can expose command execution and credential-aware paths as
well as indexing/search. Enable it only for trusted projects, inspect the tools
Maddog exposes, retain Maddog's normal permission approvals, and decide which
paths may be indexed and how long its local database may retain them. The `npx`
download happens only because this explicit plugin entry asks for it; Maddog's
core does not install or start it by default. Index original files or fetched
source material, then ask for bounded search results. Do not use it as a second
compressor for a Maddog `contextpack` result. A future native index, if justified
by evaluation, must read raw artifacts and return snippets rather than whole
command output.

## Optional Caveman Style

`caveman-full` is a response-style preference, not a data transform:

```toml
[agent]
output_style = "caveman-full"
```

It makes Maddog's own natural-language prose shorter and more direct. It does
not post-process tool results, commands, code, identifiers, quoted errors,
structured data, commits, pull-request text, approvals, or security warnings.
The setting applies when a new session builds its system prompt.

## Operating Rule

Keep `contextpack` on by default. Add `context-mode` only for durable retrieval
needs. Enable `caveman-full` only when its terse prose suits the user. Measure
completion quality and debuggability alongside token savings before broadening
any external integration.
