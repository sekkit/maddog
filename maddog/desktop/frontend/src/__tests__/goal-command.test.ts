import { goalCommandDisplay } from "../lib/goalCommand";
import type { GoalStatus } from "../lib/types";

let passed = 0;
let failed = 0;

function eq<T>(actual: T, expected: T, label: string): void {
  if (actual === expected) {
    passed += 1;
    console.log(`  PASS  ${label}`);
    return;
  }
  failed += 1;
  console.error(`  FAIL  ${label}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
}

console.log("\ngoal command display");

const budgetLimitedStatus: GoalStatus = "budget_limited";
eq(budgetLimitedStatus, "budget_limited", "wire type accepts the host budget-limited status");

const strict = goalCommandDisplay("--strict finish the migration");
eq(strict.objective, "finish the migration", "strict flag is not part of the objective");
eq(strict.hasFlags, true, "strict command reports flags");

const combined = goalCommandDisplay("--research --strict verify the release");
eq(combined.objective, "verify the release", "combined leading flags are stripped");
eq(combined.hasFlags, true, "combined command reports flags");

const plain = goalCommandDisplay("ship the release");
eq(plain.objective, "ship the release", "plain objective is unchanged");
eq(plain.hasFlags, false, "plain command reports no flags");

const embedded = goalCommandDisplay("document --strict behavior");
eq(embedded.objective, "document --strict behavior", "embedded flag text remains part of the objective");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
