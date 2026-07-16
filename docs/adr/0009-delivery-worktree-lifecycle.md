# Delivery worktrees are explicit and never auto-merge

An isolated delivery worktree is an optional branch-backed workspace protected
by the same writer lease and readiness rules as the source workspace. Maddog may
create, open, inspect, and explicitly apply or discard it, but never merges,
pushes, or deletes it automatically. Windows worktrees use short hashed paths
under Maddog-owned state, and crash recovery preserves the branch and user work.
