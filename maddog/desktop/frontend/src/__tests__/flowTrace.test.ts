import { appendTraceEntry, createTraceEntry, traceToJsonl } from "../lib/flowTrace";
import type { WireEvent } from "../lib/types";

let failed = 0;

function check(label: string, fn: () => void) {
  try {
    fn();
    process.stdout.write(`  PASS  ${label}\n`);
  } catch (err) {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}: ${(err as Error).message}\n`);
  }
}

function assert(condition: unknown, message: string) {
  if (!condition) throw new Error(message);
}

console.log("\nflowTrace");

check("preserves ordered event JSONL with summaries", () => {
  const events: WireEvent[] = [
    { kind: "turn_started" },
    { kind: "tool_dispatch", tool: { id: "1", name: "bash", args: "{\"cmd\":\"go test\"}", readOnly: false } },
    { kind: "tool_result", tool: { id: "1", name: "bash", output: "ok", readOnly: false } },
  ];
  const jsonl = traceToJsonl(events.map((event, index) => createTraceEntry(index + 1, event, 1000 + index)));
  const rows = jsonl.trim().split("\n").map((line) => JSON.parse(line));
  assert(rows.length === 3, "expected three rows");
  assert(rows[0].seq === 1 && rows[2].seq === 3, "sequence should be stable");
  assert(rows[1].summary === "tool bash dispatched", `unexpected dispatch summary ${rows[1].summary}`);
  assert(rows[2].summary === "bash completed", `unexpected result summary ${rows[2].summary}`);
});

check("redacts obvious secret keys and token-shaped values", () => {
  const entry = createTraceEntry(1, {
    kind: "tool_dispatch",
    tool: {
      id: "1",
      name: "bash",
      args: JSON.stringify({ api_key: "sk-test12345678901234567890", note: "token abcdefghijklmnopqrstuvwxyz123456" }),
      readOnly: false,
    },
  }, 1000);
  const json = traceToJsonl([entry]);
  assert(!json.includes("sk-test12345678901234567890"), "api key leaked");
  assert(!json.includes("abcdefghijklmnopqrstuvwxyz123456"), "token-shaped value leaked");
  assert(json.includes("[redacted]"), "expected redaction marker");
});

check("redacts short secrets embedded in serialized tool arguments", () => {
  const entry = createTraceEntry(1, {
    kind: "tool_dispatch",
    tool: { id: "1", name: "bash", args: JSON.stringify({ password: "short-secret", nested: { authorization: "Bearer tiny" } }), readOnly: false },
  }, 1000);
  const json = traceToJsonl([entry]);
  assert(!json.includes("short-secret"), "serialized password leaked");
  assert(!json.includes("Bearer tiny"), "serialized authorization leaked");
});

check("bounds large event strings", () => {
  const entry = createTraceEntry(1, {
    kind: "tool_result",
    tool: { id: "1", name: "bash", output: "x ".repeat(10000), readOnly: false },
  }, 1000);
  const json = traceToJsonl([entry]);
  assert(json.length < 10000, `trace entry was not bounded: ${json.length}`);
  assert(json.includes("[truncated"), "expected truncation marker");
});

check("upserts refreshed tool dispatches by ID", () => {
  let trace = appendTraceEntry([], {
    kind: "tool_dispatch",
    tool: { id: "edit-1", name: "edit_file", diff: "stale", added: 1, readOnly: false },
  }, 100);
  trace = appendTraceEntry(trace, {
    kind: "tool_dispatch",
    tool: { id: "edit-1", name: "edit_file", diff: "fresh", removed: 1, refreshed: true, readOnly: false },
  }, 100);
  assert(trace.length === 1, `expected one upserted dispatch, got ${trace.length}`);
  assert(trace[0].seq === 1, `expected stable sequence, got ${trace[0].seq}`);
  assert(trace[0].event.tool?.diff === "fresh", "refreshed preview did not replace the stale trace entry");
});

if (failed > 0) process.exit(1);
