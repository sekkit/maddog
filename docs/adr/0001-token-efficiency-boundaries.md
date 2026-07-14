# ADR 0001: Token-Efficiency Boundaries

**Status:** Accepted

**Date:** 2026-07-14

## Context

Maddog users want RTK-like routine command-output savings, context-mode-like
on-demand retrieval, and an optional Caveman-style terse response voice. These
tools operate at different boundaries. Combining them blindly can discard
debugging evidence twice and introduce an external state owner for session data.

Maddog already has a deterministic `contextpack` compressor at the tool/model
boundary, a session-scoped retained raw-result store, and an output-style prompt
layer.

## Decision

- Native `contextpack` owns routine model-visible tool-output compression and
  remains enabled by default.
- A retained raw tool result is paged through `tool_result`; large reads remain
  bounded before they return to model context when pagination is explicitly
  requested. Legacy id-only reads preserve the normal head/tail safety cap.
- External RTK is an explicit alternative, never a mandatory shell sidecar. A
  project or session using it must disable native compression manually. Maddog
  does not yet record external-transform provenance or enforce this per command.
- External context-mode is an opt-in MCP integration for independently sourced,
  searchable material. It is a source-available ELv2 dependency that is not
  auto-installed, auto-started, or used as a second compressor for command
  output. Its executable MCP surface requires an explicit trust decision.
- `caveman-full` is an optional prompt style limited to Maddog-authored natural
  language. It is Maddog-authored guidance, not copied upstream content from an
  unlicensed Caveman repository; it is not a post-processing pass and must
  preserve technical and structured artifacts. RTK is Apache-2.0.

## Consequences

The intended policy is one primary model-visible transform per result. Native
paths enforce that boundary; external RTK needs manual configuration until a
provenance contract exists. Maddog remains the owner of raw command results,
permissions, session lifecycle, and replay. External indexing requires explicit
retention and trust decisions. A future native retrieval index must consume raw
artifacts and return bounded snippets; it must not index already compressed text.
