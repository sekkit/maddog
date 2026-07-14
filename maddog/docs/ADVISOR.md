# Advisor Routing

Maddog exposes an advisor to the executor for difficult design, debugging, and
review decisions. The executor remains responsible for the task and applies the
advisor's guidance before frontier escalation.

## Native and fallback modes

| Mode | When it is used | Context path |
| --- | --- | --- |
| Native | `advisor_native_enabled = true`, and both the executor and selected advisor provider have `kind = "anthropic"`, with a compatible Claude pair | Anthropic's `advisor_20260301` server tool receives the full transcript |
| Fallback | Native is disabled or unavailable and the built-in advisor skill is enabled | Maddog runs the advisor as an isolated subagent with curated recent context |

`advisor_model` is the first choice for native routing. Only when it is empty
does native routing reuse `frontier_model` for backward compatibility. Frontier
upgrade settings such as `upgrade_enabled` and `upgrade_threshold` do not gate an
explicit `advisor_model`.

ICE GPT providers use `kind = "openai"`, including GPT traffic sent through
pxpipe. They therefore use fallback mode. An Anthropic advisor can still advise
an ICE GPT executor through fallback mode, provided both configured providers
have usable credentials.

## Model safeguards

Maddog validates model combinations it knows:

- `gpt-5.6-luna` is never accepted as an advisor, including when fallback would
  otherwise reuse it as the executor model.
- Known Anthropic native pairs are checked against Anthropic's advisor
  compatibility table. A known invalid pair is disabled before the first API
  request.
- A fallback selected through an explicit `advisor_model` is disabled when its
  known capability tier is below the executor's.
- Unknown custom or future model IDs produce a warning but are not hard-blocked.
  This keeps new models usable before Maddog's capability table is updated.

Native routing currently recognizes the Claude Haiku 4.5, Sonnet 4.6/Sonnet 5,
Opus 4.6/4.7/4.8, Fable 5, and Mythos 5 compatibility matrix. For example,
Haiku 4.5 with Opus 4.8 is valid; Opus 4.8 with Sonnet 4.6 is not. See the
[Anthropic advisor tool documentation](https://platform.claude.com/docs/en/agents-and-tools/tool-use/advisor-tool#model-compatibility)
for the upstream table.

## Configuration

```toml
[agent]
advisor_model = "anthropic-advisor/claude-opus-4-8"
advisor_max_uses_per_turn = 1
advisor_max_uses_per_session = 10
advisor_max_context_messages = 12
advisor_max_context_chars = 12000

advisor_native_enabled = true
advisor_native_max_tokens = 2048
advisor_native_cache_ttl = "5m" # empty, 5m, or 1h
```

`advisor_max_uses_per_turn = 0` disables both modes. A non-positive session cap
means unlimited session use. The message and character limits apply to fallback
context curation; native mode sends the transcript server-side under Anthropic's
advisor contract.

## Cache, tokens, and price

`advisor_native_cache_ttl` controls the native advisor's own transcript cache.
Empty disables it. Use `5m` for active loops and `1h` only when consultations
are separated by longer pauses. Cache writes generally pay off around the third
advisor call in one conversation, so short tasks should leave it empty. Keep the
TTL stable for the conversation to preserve cache hits.

`advisor_native_max_tokens` caps each native advisor sub-inference, not the
executor output. The default is 2048; Anthropic's minimum is 1024. Per-turn and
per-session use limits remain the primary call-count budget.

Native advisor usage is billed at the advisor model's rates, separately from
executor iterations. Configure the advisor provider's `price` or per-model
`prices` entry so Maddog can attribute native advisor cost correctly. Fallback
calls already use the resolved advisor provider's pricing through the subagent
path.
