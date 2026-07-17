# Recovery and Safe Mode form an offline trust boundary

The recovery guard does not load the desktop webview, plugins, MCP, hooks, bots,
sessions, custom skills, sidecars, memory learning, model upgrades, or network
services. Safe Mode starts a fresh built-in configuration with manual approval
and normal sandbox confinement. Update prepare, commit, cancel, and rollback are
one locked transaction over a verified Maddog release unit; repair operations
are limited to Maddog-owned paths and remain reversible.

## Implementation boundary

The shipped desktop entry point is `cmd/maddog-guard`. It takes an OS-backed
process lease and calls the locked startup-state guard before loading user
configuration or constructing the Wails application. At the crash-loop
threshold it exits without starting the Wails payload. The CLI recovery surface
(`maddog repair status` and
`maddog repair reset-startup`) is routed before normal configuration loading, so
it remains usable when the user configuration is corrupt. `boot.Build` also has
an explicit `SafeMode` option for offline, built-in-config recovery controllers.

`scripts/desktop-build.sh` builds that launcher without CGO for every target.
The macOS bundle keeps `maddog` as `CFBundleExecutable` and moves Wails to
`maddog-desktop`; the Windows installer and portable ZIP expose the guard as
`Maddog.exe` and install `maddog-desktop.exe` privately; Linux archives expose
`maddog` beside `maddog-desktop`, while the deb installs the guard at
`/usr/bin/maddog-desktop` and the payload under `/usr/lib/maddog`. Direct
or legacy payload launches first take the same runtime process lock; only the
primary process mutates startup state, so a second-instance restore never counts
as a crash while the primary still checks before config or Wails. Launcher and
startup-state locks use OS file locks, which the kernel releases after a crash
even though the marker file remains.

Linux publishes a legacy-compatible updater archive separately from the guarded
human-download archive. The updater archive retains `maddog` as the Wails payload
for old clients and also carries `maddog-guard` for new clients. New clients
snapshot and atomically roll back the payload and installed guard as one release
unit. A guarded update hands relaunch back to the still-running guard with a
dedicated exit code, retaining the runtime lease across the new startup check.
