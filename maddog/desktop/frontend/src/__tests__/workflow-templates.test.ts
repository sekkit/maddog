import type { WorkflowTemplateView } from "../lib/types";

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

console.log("\nworkflow templates");

const codingTask: WorkflowTemplateView = {
  schemaVersion: "v1",
  id: "coding-task",
  name: "Coding task",
  goal: "Implement a code change with readiness and verification",
  risk: "medium",
  phases: [{ id: "plan", name: "Plan", goal: "Plan implementation" }],
  providerRoles: ["default", "small", "frontier"],
  budget: { frontierTokens: 500000, totalTokens: 800000 },
  readinessGates: ["provider_configured", "credential_available"],
  humanGates: ["git_push"],
  makerChecker: { mode: "review_only" },
  requiredCapabilities: ["read", "write", "git"],
  artifacts: {
    taskPacketFields: ["request", "acceptance_criteria", "test_plan"],
    boundedFanOut: { maxParallel: 3, maxDepth: 1, requiresHumanApproval: false },
    delegationArtifacts: ["worker_summary", "files_changed", "tests_run"],
    integrationChecklist: ["merge_worker_outputs", "run_focused_tests"],
    finalVerificationArtifacts: ["run_report", "test_summary"],
    runReportMapping: [{ artifact: "final_verification", reportField: "report.finalStatus" }],
  },
  refinementStrategy: {
    enabled: true,
    searchModes: ["bfs_hypothesis", "dfs_correction"],
    critiqueRounds: 2,
    correctionRounds: 2,
    finalJudgeIsolation: "strong",
    budgetCapTokens: 100000,
    killSwitchRequired: true,
    humanApprovalRequired: true,
  },
  statePolicy: "workspace",
  maxIterations: 6,
  source: "built-in",
  sourcePath: "",
  hash: "abc123",
};

ok("template fixture exposes identity", codingTask.schemaVersion === "v1" && codingTask.id === "coding-task");
ok("template fixture exposes workflow metadata", codingTask.phases.length === 1 && codingTask.providerRoles.includes("frontier"));
ok("template fixture exposes gates and risk", codingTask.humanGates.includes("git_push") && codingTask.requiredCapabilities.includes("write"));
ok("template fixture exposes budget and provenance", codingTask.budget.frontierTokens === 500000 && codingTask.source === "built-in" && codingTask.hash.length > 0);
ok("template fixture exposes artifact review metadata", codingTask.artifacts.taskPacketFields.includes("acceptance_criteria") && codingTask.artifacts.runReportMapping.length === 1);
ok("template fixture keeps refinement default-on and gated", codingTask.refinementStrategy.enabled && codingTask.refinementStrategy.killSwitchRequired);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
