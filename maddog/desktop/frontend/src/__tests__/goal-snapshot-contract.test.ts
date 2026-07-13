import { app } from "../lib/bridge";
import { metaFromTab, sameMeta } from "../lib/useController";
import type { GoalSnapshot, Meta, TabMeta } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string): void {
  if (actual === expected) {
    passed += 1;
    console.log(`  PASS  ${label}`);
    return;
  }
  failed += 1;
  console.error(`  FAIL  ${label}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
}

console.log("\ngoal snapshot contract");

const fullSnapshot: GoalSnapshot = {
  schemaVersion: 1,
  id: "goal-1",
  objective: "finish the migration",
  goal: "finish the migration",
  status: "budget_limited",
  mode: "goal",
  researchMode: 1,
  strict: true,
  turns: 7,
  blocks: 3,
  block: "token budget reached",
  interceptMsg: "verify the remaining work",
  intercepts: 2,
  selfCheckDone: true,
  idleTurns: 1,
  turnBudget: 12,
  tokenBudget: 5000,
  tokensUsed: 5000,
  timeBudgetSeconds: 600,
  timeUsedSeconds: 45,
  lastError: "provider interrupted",
  interruptedAt: "2026-07-14T00:00:01Z",
  generation: 4,
  revision: 9,
  createdAt: "2026-07-14T00:00:00Z",
  startedAt: "2026-07-14T00:00:00Z",
  updatedAt: "2026-07-14T00:01:00Z",
  terminalAt: "2026-07-14T00:02:00Z",
  todos: [{ content: "persist the migration", status: "completed", activeForm: "Persisting the migration", level: 1 }],
};

eq(Object.keys(fullSnapshot).length, 29, "TypeScript fixture covers every GoalSnapshot JSON field");

const tab: TabMeta = {
  id: "tab-1",
  scope: "project",
  workspaceRoot: "/repo",
  workspaceName: "repo",
  topicId: "topic-1",
  topicTitle: "Goal",
  label: "model",
  ready: true,
  running: false,
  mode: "normal",
  collaborationMode: "normal",
  toolApprovalMode: "ask",
  tokenMode: "full",
  goal: "finish the migration",
  goalStatus: "budget_limited",
  goalSnapshot: fullSnapshot,
  active: true,
  cwd: "/repo",
};
const hydrated = metaFromTab(tab);
eq(hydrated.goalSnapshot?.tokensUsed, 5000, "tab hydration preserves the structured snapshot");

const baseMeta = hydrated as Meta;
eq(
  sameMeta(baseMeta, { ...baseMeta, goalSnapshot: { ...fullSnapshot, tokensUsed: 4999 } }),
  false,
  "meta equality detects nested snapshot changes",
);
eq(
  sameMeta(baseMeta, { ...baseMeta, goalSnapshot: { ...fullSnapshot, todos: fullSnapshot.todos?.map((todo) => ({ ...todo })) } }),
  true,
  "meta equality accepts an equivalent snapshot copy",
);

const mockMeta = await app.Meta();
eq(mockMeta.goalSnapshot?.schemaVersion, 1, "dev mock meta exposes a versioned snapshot");
const mockTabs = await app.ListTabs();
eq(mockTabs.every((item) => item.goalSnapshot?.schemaVersion === 1), true, "dev mock tabs expose versioned snapshots");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
