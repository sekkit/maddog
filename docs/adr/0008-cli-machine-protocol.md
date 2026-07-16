# CLI automation uses a versioned machine protocol

Maddog one-shot automation exposes `text`, `json`, and `stream-json` output with
machine data only on stdout, diagnostics on stderr, stable exit codes, and a
versioned result schema. Session flags such as allowed tools and additional
directories may only restrict or explicitly extend the current workspace
confinement; they never bypass deny rules, presence-required approval, MCP trust,
credential scope, or forbidden read roots.
