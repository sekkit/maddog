# Maddog Context

## Token-Efficiency Vocabulary

- **Raw tool result:** The exact text returned by a Maddog tool before a
  model-visible transform. When native compression saves context, it is retained
  in a session-scoped artifact addressed by `raw://tool/<call-id>`.
- **Model-visible tool output:** The one bounded representation of a tool result
  sent to the provider. Native code knows whether it is raw or natively
  compressed. An external transform is a manual policy boundary until Maddog
  records transform provenance, so it can otherwise be compressed twice.
- **External indexed context:** Source material stored and searched by an
  opt-in system outside Maddog's session artifact store, such as context-mode.
  It is not an authoritative copy of tool output.
- **Response style:** Prompt guidance that affects only Maddog-authored
  natural-language prose. It must not rewrite tool payloads or structured
  artifacts.

## Upstream Adoption Vocabulary

- **Maddog-relevant upstream feature:** A Reasonix-originated capability that
  strengthens Maddog's local agent, desktop, CLI, security, provider, or
  developer experience without depending on Reasonix-branded cloud,
  community, account, website, or release surfaces.
  _Avoid:_ All upstream features, full upstream parity

- **Maddog trust root:** The Maddog-controlled authority that determines which
  distributed capability definitions may receive official trust.
  _Avoid:_ Reasonix official trust, upstream trust

- **Capability receipt:** A durable local record that a user approved one
  specific capability identity and reviewed capability snapshot.
  _Avoid:_ Allowlist entry, permanent approval

- **Strict Reader Execution:** An execution posture that permits observation
  but forbids durable workspace or user-state mutation; trusted readers may
  write only to their private runtime state.
  _Avoid:_ Read-only hint, safe shell

- **Session Runtime Lease:** Exclusive authority for one runtime to write a
  canonical session transcript path.
  _Avoid:_ PID file, active-tab ownership

- **Transcript CAS Baseline:** The last persisted transcript revision and
  digest that a writer must still match before replacing durable history.
  _Avoid:_ Last save time

- **Recovery Branch:** A bounded alternate transcript that preserves work when
  a stale or divergent writer cannot safely update the requested session.
  _Avoid:_ Conflict overwrite, backup copy
