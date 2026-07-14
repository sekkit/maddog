# pxpipe Gateway

Maddog can use [pxpipe](https://github.com/teamchong/pxpipe) as an optional
local request-compression sidecar. Maddog still owns provider selection,
history, tools, compaction, credentials, and UI state. pxpipe owns request-body
transforms, model allowlisting, image rendering, upstream forwarding, and its
dashboard/event log.

The integration is opt-in. Normal Maddog providers do not require Node or
pxpipe.

## When To Use It

pxpipe can reduce token load for token-dense, stable context on models that read
the rendered image blocks well. It is not a byte-perfect memory system.

Keep these as text, or use a non-imaged pass-through model:

- exact identifiers, hashes, filenames, and commands
- secrets, tokens, and credentials
- recent user turns
- failure diagnostics where exact wording matters
- source snippets you expect the model to quote exactly

pxpipe decides whether a request is transformed or passed through. Maddog does
not claim savings unless pxpipe/Maddog telemetry proves it for the session.

## Managed Mode

Configure the local sidecar once. When a `pxpipe-*` provider is selected, Maddog
then provisions the pinned `pxpipe-proxy` package through `npx` (when needed),
starts the loopback sidecar, waits for it to become healthy, and only then sends
the model request. No separate `npx pxpipe-proxy` terminal is required.

```toml
[pxpipe]
enabled = true
auto_install = true
auto_start = true
models = ["gpt-5.6"]
openai_upstream = "https://your-openai-compatible-gateway.example"
```

For example, selecting `pxpipe-gpt/gpt-5.6` with this configuration is enough:

```sh
maddog run --model pxpipe-gpt/gpt-5.6 "your task"
```

The lifecycle commands remain useful for diagnostics and explicit stop/restart:

```sh
maddog pxpipe status
maddog pxpipe start
maddog pxpipe stop
```

Command-line flags temporarily override `[pxpipe]`:

```sh
maddog pxpipe start \
  --models claude-fable-5,gpt-5.6 \
  --anthropic-upstream https://api.anthropic.com \
  --openai-upstream https://api.openai.com/v1
```

Defaults:

- `HOST=127.0.0.1`
- `PORT=47821`
- `PXPIPE_MODELS` from `[pxpipe].models` (default: `claude-fable-5,gpt-5.6`)
- `PXPIPE_LOG=<maddog-state>/pxpipe/events.jsonl`

`[pxpipe].anthropic_upstream` and `[pxpipe].openai_upstream` are the persistent
source of truth. If either is omitted, Maddog derives a non-loopback upstream from
configured providers as a compatibility fallback. Set persistent values explicitly
for aggregator or regional gateways so pxpipe never falls back to a first-party
endpoint accidentally.

One pxpipe process has only one `OPENAI_UPSTREAM`. When two providers use
different OpenAI-compatible gateways, bind the second provider to a dedicated
managed instance instead of pointing both providers at the same port:

```toml
[[pxpipe.instances]]
name = "sevnx"
providers = ["pxpipe-sevnx-gpt"]
host = "127.0.0.1"
port = 47822
models = ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"]
openai_upstream = "https://www.sevnx.one"
```

Selecting `pxpipe-sevnx-gpt/*` automatically provisions and starts port 47822.
Each non-default port gets its own PID state, runner log, and pxpipe event log.
The legacy fields directly under `[pxpipe]` remain the default instance.

`maddog doctor` reports pxpipe state, dashboard URL, provider loopback checks,
and log file size. It does not read or print pxpipe event contents.

## Manual Mode

You can also run pxpipe yourself:

```sh
ANTHROPIC_UPSTREAM=https://api.anthropic.com \
OPENAI_UPSTREAM=https://api.openai.com/v1 \
PXPIPE_MODELS=claude-fable-5,gpt-5.6 \
npx pxpipe-proxy
```

For non-first-party gateways, replace the upstream URLs with the actual provider
roots. Keep API keys in environment variables referenced by Maddog's
`api_key_env`; do not put keys in TOML.

## Provider Config

Anthropic/Fable path:

```toml
[[providers]]
name = "pxpipe-claude"
kind = "anthropic"
base_url = "http://127.0.0.1:47821"
model = "claude-fable-5"
api_key_env = "ANTHROPIC_API_KEY"
context_window = 1000000
thinking = "adaptive"
effort = "high"
no_proxy = true
```

OpenAI Responses path:

```toml
[[providers]]
name = "pxpipe-gpt"
kind = "openai"
base_url = "http://127.0.0.1:47821/v1"
models = ["gpt-5.6"]
default = "gpt-5.6"
api_key_env = "OPENAI_API_KEY"
context_window = 1000000
wire_api = "responses"
reasoning_protocol = "openai"
supported_efforts = ["low", "medium", "high"]
default_effort = "high"
no_proxy = true
```

`no_proxy = true` keeps localhost traffic out of corporate or regional outbound
proxies.

## Model Scope

Do not silently image unsupported models. pxpipe's safe default scope is narrow;
models outside `PXPIPE_MODELS` should pass through unchanged unless you opt in.

In particular, GPT 5.5 is not a safe default in pxpipe's current guidance. Use
GPT 5.6/Fable-class models by default, or set `PXPIPE_MODELS=off` to force
pass-through while keeping the same proxy route.

For GPT, Maddog only treats `pxpipe-gpt` as selectable when a model is explicitly
configured or discovered. A provider with no `model`, `models`, or `models_url`
is diagnosed as missing model configuration instead of constructing a client with
an empty model.

## Troubleshooting

```sh
maddog pxpipe status --json
maddog doctor --json
```

Common states:

- `not-installed`: install `pxpipe` or make `npx` available.
- `not-running`: start pxpipe manually or run `maddog pxpipe start`.
- `running-unmanaged`: something is already responding on the pxpipe port, but
  Maddog did not start it.
- `running-managed`: Maddog started the sidecar and has a state file for it.
- `unhealthy`: the process was started but did not answer the dashboard probe.

The dashboard is unauthenticated, so Maddog-managed mode binds to
`127.0.0.1` by default.
