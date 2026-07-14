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
