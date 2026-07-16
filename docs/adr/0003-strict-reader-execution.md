# Strict Reader Execution is immutable and revalidated at dispatch

A planner, reviewer, or reader subagent is constructed through one strict
reader boundary that fixes its execution intent and final tool registry.
Resolved tools, including proxy targets and MCP readers, are revalidated against
live trust and capability identity at the dispatch linearization point. Missing,
stale, destructive, or untrusted state fails before process, network, or target
execution; generic shell execution is not classified as a strict reader.
