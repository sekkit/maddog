import type { WireEvent } from "../lib/types";
import { app } from "../lib/bridge";

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

console.log("\nmaker-checker wire contract");

const makerChecker = {
  kind: "maker_checker",
  level: "warn",
  makerChecker: {
    mode: "enforced_before_done",
    verdict: "changes_requested",
    isolation: "strong",
    canComplete: false,
    retryAllowed: true,
  },
} satisfies WireEvent;

const humanGate = {
  kind: "human_gate",
  level: "warn",
  humanGate: {
    kind: "git_push",
    required: true,
    status: "needs_human",
    reason: "git push requires approval",
  },
} satisfies WireEvent;

ok("maker-checker event carries verdict", makerChecker.makerChecker.verdict === "changes_requested");
ok("human gate event carries kind", humanGate.humanGate.kind === "git_push");

const templates = await app.WorkflowTemplates();
const codingTask = templates.find((item) => item.id === "coding-task");
ok("coding-task refinement strategy is default-off and gated", Boolean(
  codingTask?.refinementStrategy.enabled === false
  && codingTask.refinementStrategy.searchModes.includes("bfs_hypothesis")
  && codingTask.refinementStrategy.searchModes.includes("dfs_correction")
  && codingTask.refinementStrategy.killSwitchRequired
  && codingTask.refinementStrategy.humanApprovalRequired
  && codingTask.refinementStrategy.budgetCapTokens > 0
));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
