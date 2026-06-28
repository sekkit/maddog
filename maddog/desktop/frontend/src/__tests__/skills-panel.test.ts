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

console.log("\nskill candidate GUI contract");

const before = await app.Capabilities();
const candidate = before.skillCandidates.find((item) => item.id === "cand-dynamic-docs");
ok("capabilities exposes pending skill candidate", Boolean(candidate && candidate.status === "pending" && candidate.decision === "promotable"));

const promoted = await app.PromoteSkillCandidate("cand-dynamic-docs");
ok(
  "promote binding returns promoted candidate",
  promoted.status === "promoted" && typeof promoted.promotedPath === "string" && promoted.promotedPath.length > 0,
);

const afterPromote = await app.Capabilities();
ok("promoted candidate remains traceable", Boolean(afterPromote.skillCandidates.find((item) => item.id === "cand-dynamic-docs" && item.status === "promoted")));

await app.RollbackPromotedSkill("cand-dynamic-docs");
const afterRollback = await app.Capabilities();
ok("rollback restores candidate to pending", Boolean(afterRollback.skillCandidates.find((item) => item.id === "cand-dynamic-docs" && item.status === "pending")));

const rejected = await app.RejectSkillCandidate("cand-dynamic-docs", "needs more replay coverage");
ok("reject binding records reason", rejected.status === "rejected" && typeof rejected.reason === "string" && rejected.reason.includes("replay"));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
