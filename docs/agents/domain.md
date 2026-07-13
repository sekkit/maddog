# Domain Docs

This repository uses a single-context domain documentation layout.

## Before exploring

Read the following when they exist and are relevant to the work:

- `CONTEXT.md` at the repository root.
- ADRs under `docs/adr/`.

If either location does not exist, proceed silently. Domain-modeling workflows
create these files lazily when terminology or architectural decisions are
actually resolved.

## Layout

```text
/
|-- CONTEXT.md
|-- docs/
|   `-- adr/
`-- ...
```

## Vocabulary

Use terms as defined by the glossary in `CONTEXT.md` in issue titles, plans,
tests, and implementation notes. If a needed concept is absent, first check
whether the codebase already uses a stable term. Record a real terminology gap
for domain modeling rather than inventing competing synonyms.

## ADR conflicts

Surface any proposal that contradicts an existing ADR. Name the ADR and explain
why reopening the decision may be justified instead of silently overriding it.
