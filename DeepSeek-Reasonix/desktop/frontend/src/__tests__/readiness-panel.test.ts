import { app } from "../lib/bridge";
import { initialState, reducer } from "../lib/useController";
import type { ReadinessResultView, WireEvent } from "../lib/types";

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

console.log("\nworkflow readiness contract");

const readiness = await app.WorkflowReadiness("coding-task");
ok("mock app exposes workflow readiness", readiness.templateId === "coding-task");
ok("mock readiness returns a valid status", ["ready", "warning", "blocked", "needs_approval"].includes(readiness.status));
ok("mock readiness includes checks", readiness.checks.length > 0);

const event: WireEvent = {
  kind: "readiness",
  level: "warn",
  readiness: {
    status: "blocked",
    score: 50,
    templateId: "coding-task",
    checks: [{ id: "credential_available", status: "blocked", credentialEnv: "OPENAI_API_KEY" }],
    blockers: ["credential unavailable"],
  } satisfies ReadinessResultView,
};

ok("wire event carries readiness payload", event.readiness?.checks[0]?.credentialEnv === "OPENAI_API_KEY");
ok("wire readiness event has warning level", event.level === "warn");

const reduced = reducer(initialState, { type: "event", e: event });
ok("reducer stores latest readiness", reduced.readiness?.status === "blocked");
ok("reducer adds transcript readiness item", reduced.items.some((item) => item.kind === "readiness" && item.readiness.status === "blocked"));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
