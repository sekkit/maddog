# Recovery and Safe Mode form an offline trust boundary

The recovery guard does not load the desktop webview, plugins, MCP, hooks, bots,
sessions, custom skills, sidecars, memory learning, model upgrades, or network
services. Safe Mode starts a fresh built-in configuration with manual approval
and normal sandbox confinement. Update prepare, commit, cancel, and rollback are
one locked transaction over a verified Maddog release unit; repair operations
are limited to Maddog-owned paths and remain reversible.
