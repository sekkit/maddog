# Recovery and Safe Mode form an offline trust boundary

The recovery guard does not load the desktop webview, plugins, MCP, hooks, bots,
sessions, custom skills, sidecars, memory learning, model upgrades, or network
services. Safe Mode starts a fresh built-in configuration with manual approval
and normal sandbox confinement. Update prepare, commit, cancel, and rollback are
one locked transaction over a verified Maddog release unit; repair operations
are limited to Maddog-owned paths and remain reversible.

## Implementation boundary

The shipped desktop executable is the recovery guard. `desktop/main.go` calls
the locked startup-state guard before loading user configuration or constructing
the Wails application, and exits without creating a webview when the crash-loop
threshold is reached. The CLI recovery surface (`maddog repair status` and
`maddog repair reset-startup`) is routed before normal configuration loading, so
it remains usable when the user configuration is corrupt. `boot.Build` also has
an explicit `SafeMode` option for offline, built-in-config recovery controllers.

An additional launcher binary is not part of the current release contract. The
release scripts produce the Wails executable directly, the Windows installer
launches that executable, and the updater starts the platform installer/helper;
there is no cross-platform wrapper that is actually shipped and invoked. A
standalone guard that is not wired into those paths would create a false trust
boundary. The executable pre-Wails check is therefore sufficient for ADR 0007.

Release-only hardening remains tracked separately: package a platform launcher
only when each `desktop-build.sh` target, NSIS installer, macOS app bundle, and
Linux archive/deb invokes it; add signed launcher artifacts and installer smoke
tests; and ensure updater rollback restores the launcher and executable as one
release unit. None of those packaging changes are implied by this ADR or by the
current source-only implementation.
