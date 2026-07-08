# Environment Registry

Maddog can persist project-local tool resolution facts so repeated sessions do not
keep rediscovering the same binaries from scratch.

## What it stores

For each supported tool, Maddog records:

- selected executable path
- detected version
- expected version, when Maddog knows one
- resolution source (`override`, cached registry, Go bin dir, or `PATH`)
- failed candidates and last error
- last verification time

The registry is machine-derived state, not hand-authored configuration.

## Where it lives

Maddog stores the registry under the project state tree:

```text
<MADDOG_STATE_HOME>/projects/<workspace-slug>/environment/registry.json
```

If `MADDOG_STATE_HOME` is unset, Maddog uses its normal state home convention.

## Supported tools

Current built-in resolvers cover:

- `go`
- `pxpipe`
- `npx`
- `pnpm`
- `wails`
- `create-dmg`
- `nfpm`
- `makensis`

## Commands

```text
maddog env list [--json]
maddog env refresh [--json]
maddog env show --tool NAME
```

## Overrides

Specific tools can be pinned with override environment variables:

```text
MADDOG_GO_PATH
MADDOG_PXPIPE_PATH
MADDOG_NPX_PATH
MADDOG_PNPM_PATH
MADDOG_WAILS_PATH
MADDOG_CREATE_DMG_PATH
MADDOG_NFPM_PATH
MADDOG_MAKENSIS_PATH
```

## Current consumers

Today the shared registry is consumed by:

- `maddog doctor`
- `maddog env`
- `prod_test`
- `cmd/maddog-env`

Additional tool-discovery call sites can migrate to the same registry over time.
