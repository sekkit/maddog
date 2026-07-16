# Reasonix adoption expert decisions

## Scope

Implement every Maddog-relevant P0/P1 feature and the approved P2 automation,
image preview, and delivery-worktree UX. Keep hard exclusions for Reasonix
branding, website/release channels, accounts/community/admin, Feishu God View,
and upstream signing or download infrastructure.

## Already merged and retained

- Historical message timestamp overlay without changing provider transcripts.
- Pinned projects/topics/conversations.
- Pending approval snapshot and tab-scoped replay across session navigation.
- Maddog typed provider errors and existing tab generation guards, which remain
  foundations rather than complete substitutes for the new work.

## Required implementation batches

1. Credential environment isolation and private artifact storage, including
   Windows DACLs.
2. MCP identity, receipts, Maddog-signed catalog, immutable launchers, live
   revocation, same-origin redirect policy, and Strict Reader Execution.
3. Session Runtime Lease, transcript CAS, recovery branches, atomic controller
   replacement, and tab-scoped generation checks.
4. Todo serial state, 8/16 adaptive progress guard, explicit budget taxonomy,
   grounded edit receipts, refreshed previews, and presence-required approval.
5. Provider explicit-zero temperature, defensive empty schemas, root
   `properties:{}`, and cumulative Anthropic usage.
6. Windows plugin-hook compatibility and sanitized MCP npm endpoint diagnostics.
7. Offline repair core, Safe Mode, guard packaging, and locked updater rollback.
8. Delivery worktree kernel and explicit desktop lifecycle UI.
9. Image lightbox and versioned CLI text/JSON/NDJSON automation protocol.

## Merge definition

A feature is merged only when its test-first commit is reachable from the
`codex/reasonix-adoption` integration branch, its focused and cross-boundary
tests pass, branding scans are clean, and its status/evidence is recorded here.
The dirty root worktree is never committed or overwritten by this effort.

## Current status

- **Complete on this branch:** credential isolation across configured and stale
  stores, dynamic registration, background launchers, private transcript/job
  storage with Unix permissions and Windows DACL helpers, symlink/reparse
  guards, Windows plugin hooks, sanitized npm endpoint diagnostics, explicit
  zero-temperature provider semantics, defensive root schemas, and cumulative
  Anthropic usage.
- **Verified existing:** historical timestamp display, pinned conversations,
  pending approval replay, and existing typed provider errors.
- **Next:** MCP trust/receipt/catalog and Strict Reader Execution; session
  lease/CAS; todo/progress/approval state machine; recovery/Safe Mode; delivery
  worktree; image lightbox; CLI machine protocol.
- **Known baseline failures:** `internal/control.TestSlashArgItems` expects an
  obsolete `/effort` option set; the full desktop suite exceeded the local
  timeout. Neither failure was introduced by the completed batches.
