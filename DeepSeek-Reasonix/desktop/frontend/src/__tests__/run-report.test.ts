import { initialState, reducer } from "../lib/useController";
import { DEFAULT_STATUS_BAR_ITEMS } from "../lib/statusBarItems";
import type { RunReportView, WireEvent } from "../lib/types";

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

console.log("\nrun report contract");

const report: RunReportView = {
  runId: "run-1",
  loopId: "coding-task",
  status: "completed",
  path: "C:/Users/Sekkit/AppData/Roaming/maddog/runs/run-1.jsonl",
  events: 4,
};

const event: WireEvent = {
  kind: "run_report_ready",
  level: "info",
  runReport: report,
};

ok("wire event carries run report payload", event.runReport?.runId === "run-1" && event.runReport.events === 4);
ok("run report is visible by default", DEFAULT_STATUS_BAR_ITEMS.includes("run_report"));

const reduced = reducer(initialState, { type: "event", e: event });
ok("reducer stores latest run report", reduced.runReport?.status === "completed");
ok("reducer adds transcript run report item", reduced.items.some((item) => item.kind === "run_report" && item.runReport.runId === "run-1"));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
