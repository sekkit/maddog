# HyperGraphRAG Backend

Maddog can register HyperGraphRAG as an optional code-intelligence backend for semantic search and context-pack retrieval. It is not a replacement for CodeGraph, grep, file reads, or LSP. The integration is sidecar based so Python, embedding, model, and storage dependencies stay outside the Maddog Go process and outside the default startup path.

## Configuration

```toml
[[code_intelligence.backends]]
name = "project-hypergraph"
kind = "hypergraphrag"
command = "maddog-hypergraphrag"
args = ["--workdir", ".maddog/hypergraph"]
enabled = true

[code_intelligence.backends.env]
OPENAI_API_KEY = "${OPENAI_API_KEY}"
```

Inspect configured backends without launching the sidecar:

```sh
maddog hypergraphrag status
```

The status command prints environment variable names only. It does not print configured values.

## Sidecar Contract

Maddog invokes the configured command with these subcommands:

```sh
maddog-hypergraphrag health --json
maddog-hypergraphrag index --root <repo-root> --json
maddog-hypergraphrag query --capability semantic_search --query <text> --json --top-k 5
maddog-hypergraphrag query --capability context_pack --query <text> --json --budget-tokens 4000
```

Health response:

```json
{ "status": "ready" }
```

Query response:

```json
{
  "results": [
    {
      "id": "docs/cc/example/tech.md",
      "title": "Technical plan",
      "content": "short, source-backed context"
    }
  ]
}
```

## Where It Helps

Use HyperGraphRAG for relationship-heavy retrieval: linking docs, plans, research notes, historical decisions, and code concepts into an evidence pack for review, planning, or benchmarked code intelligence. Keep precise symbol lookup and exact text search on CodeGraph, LSP, grep, and file tools.

Benchmark it with:

```sh
go run ./cmd/codeintelbench --repo .
```
