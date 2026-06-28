import { app } from "../lib/bridge";
import { SETTINGS_TABS } from "../lib/settingsTabs";

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

console.log("\nsettings workflows contract");

ok("settings exposes workflows tab", SETTINGS_TABS.includes("workflows" as never));

const templates = await app.WorkflowTemplates();
const codingTask = templates.find((item) => item.id === "coding-task");
ok("workflow templates include coding-task", Boolean(codingTask));
ok("coding-task exposes roles and gates", Boolean(codingTask?.providerRoles.includes("frontier") && codingTask.humanGates.length > 0));
ok("coding-task exposes workflow artifact contract", Boolean(
  codingTask?.artifacts.taskPacketFields.includes("acceptance_criteria")
  && (codingTask.artifacts.boundedFanOut?.maxParallel ?? 0) > 0
  && codingTask.artifacts.finalVerificationArtifacts.includes("run_report")
));

const readiness = await app.WorkflowReadiness("coding-task");
ok("workflow readiness can be refreshed from settings", readiness.templateId === "coding-task" && readiness.checks.length > 0);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
