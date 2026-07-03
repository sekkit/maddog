// Run: tsx src/__tests__/capabilities-skill-candidates.test.ts

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

let passed = 0;
let failed = 0;

function source(path: string): string {
  return readFileSync(fileURLToPath(new URL(path, import.meta.url)), "utf8");
}

function check(label: string, ok: boolean) {
  if (ok) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
    return;
  }
  process.stdout.write(`  FAIL  ${label}\n`);
  failed += 1;
}

function includesAll(body: string, needles: string[]): boolean {
  return needles.every((needle) => body.includes(needle));
}

const panel = source("../components/CapabilitiesPanel.tsx");
const bridge = source("../lib/bridge.ts");
const types = source("../lib/types.ts");
const en = source("../locales/en.ts");
const zh = source("../locales/zh.ts");
const zhTW = source("../locales/zh-TW.ts");

console.log("\ncapabilities skill candidates");

check(
  "wire types carry skill candidate audit fields",
  includesAll(types, [
    "export interface SkillCandidateView",
    'status: "pending" | "promoting" | "rejected" | "promoted" | "rolled_back" | string;',
    "sourceTask?: string;",
    "sourceBundleId?: string;",
    "promotedPath?: string;",
    "targetRoot?: string;",
    "score?: number;",
    "guardrailPass?: boolean;",
    "audit?: SkillCandidateAuditView[];",
    "export interface SkillCandidateAuditView",
    "skillCandidates?: SkillCandidateView[];",
  ]),
);

check(
  "bridge exposes candidate promote/reject actions and dev mock state",
  includesAll(bridge, [
    "SkillCandidateView",
    "EvaluateSkillCandidate(hash: string): Promise<SkillCandidateView>",
    "PromoteSkillCandidate(hash: string): Promise<string>",
    "RollbackSkillCandidate(hash: string, reason: string): Promise<void>",
    "RejectSkillCandidate(hash: string, reason: string): Promise<void>",
    "capSkillCandidates",
    "skillCandidates: capSkillCandidates.map",
    "async EvaluateSkillCandidate(hash: string)",
    "async PromoteSkillCandidate(hash: string)",
    "async RollbackSkillCandidate(hash: string, reason: string)",
    "async RejectSkillCandidate(hash: string, reason: string)",
  ]),
);

check(
  "capabilities panel renders candidate audit queue and actions",
  includesAll(panel, [
    "SkillCandidatesSection",
    "SkillCandidateRow",
    "candidates={view.skillCandidates ?? []}",
    "app.EvaluateSkillCandidate(hash)",
    "onEvaluateAll",
    "app.PromoteSkillCandidate(hash)",
    "app.RollbackSkillCandidate(hash, t(\"caps.skillCandidateRollbackReason\"))",
    "app.RejectSkillCandidate(hash, t(\"caps.skillCandidateRejectReason\"))",
    "onEvaluate",
    "canEvaluate",
    "canEvaluateSkillCandidate",
    "skillCandidateFilterLabel",
    "caps.skillCandidates",
    "caps.skillCandidatesEmpty",
    "caps.skillCandidateScore",
    "caps.skillCandidateEvaluate",
    "caps.skillCandidateEvaluateAll",
    "caps.skillCandidatePromote",
    "caps.skillCandidateRollback",
    "caps.skillCandidateReject",
    "skillCandidateStatusLabel",
    "skillCandidateAuditActionLabel",
    "caps.skillCandidateAudit",
  ]),
);

check(
  "English and Chinese locales include candidate labels",
  includesAll(en, [
    '"caps.skillCandidates":',
    '"caps.skillCandidatesSummary":',
    '"caps.skillCandidatesEmpty":',
    '"caps.skillCandidateStatus.pending":',
    '"caps.skillCandidateStatus.promoted":',
    '"caps.skillCandidateStatus.rolledBack":',
    '"caps.skillCandidateScore":',
    '"caps.skillCandidateEvaluate":',
    '"caps.skillCandidateEvaluateAll":',
    '"caps.skillCandidatePromote":',
    '"caps.skillCandidateRollback":',
    '"caps.skillCandidateReject":',
    '"caps.skillCandidateAudit":',
    '"caps.skillCandidateAudit.promote":',
    '"caps.skillCandidateAudit.rollback":',
  ]) && includesAll(zh, [
    '"caps.skillCandidates":',
    '"caps.skillCandidatesSummary":',
    '"caps.skillCandidatesEmpty":',
    '"caps.skillCandidateStatus.pending":',
    '"caps.skillCandidateStatus.promoted":',
    '"caps.skillCandidateStatus.rolledBack":',
    '"caps.skillCandidateScore":',
    '"caps.skillCandidateEvaluate":',
    '"caps.skillCandidateEvaluateAll":',
    '"caps.skillCandidatePromote":',
    '"caps.skillCandidateRollback":',
    '"caps.skillCandidateReject":',
    '"caps.skillCandidateAudit":',
    '"caps.skillCandidateAudit.promote":',
    '"caps.skillCandidateAudit.rollback":',
  ]) && includesAll(zhTW, [
    '"caps.skillCandidates":',
    '"caps.skillCandidatesSummary":',
    '"caps.skillCandidatesEmpty":',
    '"caps.skillCandidateStatus.pending":',
    '"caps.skillCandidateStatus.promoted":',
    '"caps.skillCandidateStatus.rolledBack":',
    '"caps.skillCandidateScore":',
    '"caps.skillCandidateEvaluate":',
    '"caps.skillCandidateEvaluateAll":',
    '"caps.skillCandidatePromote":',
    '"caps.skillCandidateRollback":',
    '"caps.skillCandidateReject":',
    '"caps.skillCandidateAudit":',
    '"caps.skillCandidateAudit.promote":',
    '"caps.skillCandidateAudit.rollback":',
  ]),
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
