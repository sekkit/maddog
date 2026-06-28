import { initialState, reducer, type Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

let passed = 0;
let failed = 0;

function ok(label: string, value: boolean) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
    return;
  }
  process.stdout.write(`  FAIL  ${label}\n`);
  failed += 1;
}

function toolItems(items: Item[]): ToolItem[] {
  return items.filter((item): item is ToolItem => item.kind === "tool");
}

console.log("\ntool compression contract");

let state = reducer(initialState, {
  type: "event",
  e: { kind: "tool_dispatch", tool: { id: "call-1", name: "bash", args: "{}", readOnly: true } },
});
state = reducer(state, {
  type: "event",
  e: {
    kind: "tool_result",
    tool: {
      id: "call-1",
      name: "bash",
      readOnly: true,
      output: "raw output",
      compression: {
        compressed: true,
        strategy: "deterministic_head_tail_errors",
        rawRef: "tool://call-1/raw",
        originalBytes: 1000,
        compressedBytes: 300,
        savedBytes: 700,
      },
    },
  },
});

const tool = toolItems(state.items)[0];
ok("tool item exists", Boolean(tool));
ok("tool output is still archived", tool?.dataArchived === true && tool?.output === undefined);
ok("compression metrics are stored", tool?.compression?.savedBytes === 700 && tool.compression.rawRef === "tool://call-1/raw");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
