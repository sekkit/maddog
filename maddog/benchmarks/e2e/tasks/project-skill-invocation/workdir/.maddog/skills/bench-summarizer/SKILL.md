---
name: bench-summarizer
description: Produces the deterministic Maddog benchmark summary string.
allowed-tools: read_file
---

# Bench Summarizer

Read the requested source file and extract the values for project, mechanism,
and status. Return exactly:

`MADDOG-BENCH:<project>|<mechanism>|<status>`

Do not add explanation, bullets, punctuation, or extra whitespace.
