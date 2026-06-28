import { initialState, reducer } from "../lib/useController";
import type { HumanGateResultView, MakerCheckerResultView, RunReportView, WireEvent } from "../lib/types";

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

console.log("\nloop control surface contract");

const checker: MakerCheckerResultView = {
  mode: "enforced_before_done",
  verdict: "approved",
  isolation: "weak",
  canComplete: true,
  makerProvider: "deepseek",
  makerModel: "deepseek-v4-flash",
  checkerProvider: "openai-official",
  checkerModel: "gpt-5",
};

const humanGate: HumanGateResultView = {
  kind: "git_push",
  required: true,
  status: "pending",
  reason: "git push requires approval",
};

const report: RunReportView = {
  runId: "run-1",
  loopId: "coding-task",
  templateId: "coding-task",
  finalStatus: "completed",
  status: "completed",
  path: "C:/Users/Sekkit/AppData/Roaming/maddog/runs/run-1/run.jsonl",
  reportPath: "C:/Users/Sekkit/AppData/Roaming/maddog/runs/run-1/report.json",
  events: 7,
  phases: [{ id: "readiness", status: "completed" }, { id: "execute", status: "completed" }],
  models: [{ role: "frontier", provider: "openai-official", model: "gpt-5", totalTokens: 1200, cost: 0.42, currency: "$", upgradeReason: "low confidence" }],
  budget: { usedTokens: 1200, limitTokens: 2000, remainingTokens: 800, cost: 0.42, currency: "$" },
  readiness: { status: "ready", score: 100, checks: [], templateId: "coding-task" },
  checker,
  humanGate,
};

const withReport = reducer(initialState, { type: "event", e: { kind: "run_report_ready", runReport: report } satisfies WireEvent });
ok("reducer stores detailed run report", withReport.runReport?.templateId === "coding-task" && withReport.runReport.budget?.remainingTokens === 800);
ok("run report item is added to transcript", withReport.items.some((item) => item.kind === "run_report" && item.runReport.reportPath?.endsWith("report.json")));

const withGate = reducer(withReport, { type: "event", e: { kind: "human_gate", humanGate } satisfies WireEvent });
ok("pending human gate remains available as live state", withGate.humanGate?.status === "pending" && withGate.humanGate.kind === "git_push");

const withChecker = reducer(withGate, { type: "event", e: { kind: "maker_checker", makerChecker: checker } satisfies WireEvent });
ok("maker checker remains available as live state", withChecker.makerChecker?.verdict === "approved" && withChecker.makerChecker.isolation === "weak");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
