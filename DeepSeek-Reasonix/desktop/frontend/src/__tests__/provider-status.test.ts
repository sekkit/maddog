import { initialState, reducer } from "../lib/useController";
import { DEFAULT_STATUS_BAR_ITEMS } from "../lib/statusBarItems";
import type { ProviderStatusView, WireEvent } from "../lib/types";

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

console.log("\nprovider status contract");

const providerStatus: ProviderStatusView = {
  role: "frontier",
  provider: "anthropic-frontier",
  model: "anthropic-frontier/claude-sonnet-4",
  status: "active",
  upgradeReason: "2 consecutive tool failures",
  requestCount: 1,
  promptTokens: 50,
  completionTokens: 7,
  totalTokens: 57,
  cost: 0.001,
  currency: "$",
  budgetUsedTokens: 7,
  budgetLimitTokens: 10,
  budgetRemainingTokens: 3,
};

const event: WireEvent = {
  kind: "provider_status",
  level: "info",
  providerStatus,
};

ok("wire event carries provider status payload", event.providerStatus?.role === "frontier" && event.providerStatus.budgetRemainingTokens === 3);
ok("provider status is visible by default", DEFAULT_STATUS_BAR_ITEMS.includes("provider_status"));

const reduced = reducer(initialState, { type: "event", e: event });
ok("reducer stores latest provider status", reduced.providerStatus?.role === "frontier");
ok("reducer adds transcript provider status item", reduced.items.some((item) => item.kind === "provider_status" && item.providerStatus.model === "anthropic-frontier/claude-sonnet-4"));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
