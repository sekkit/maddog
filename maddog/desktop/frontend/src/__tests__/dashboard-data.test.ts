// Run: tsx src/__tests__/dashboard-data.test.ts

import {
  DASHBOARD_FEATURE_GROUPS,
  DASHBOARD_TELEMETRY_SIGNALS,
  buildDashboardMetrics,
  buildTelemetryStatus,
  flattenDashboardFeatures,
} from "../lib/dashboardData";

let passed = 0;
let failed = 0;

function check(label: string, ok: boolean) {
  if (ok) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
    return;
  }
  process.stdout.write(`  FAIL  ${label}\n`);
  failed += 1;
}

function eq(a: unknown, b: unknown, label: string) {
  check(`${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`, JSON.stringify(a) === JSON.stringify(b));
}

console.log("\ndashboard data");

const featureIds = flattenDashboardFeatures(DASHBOARD_FEATURE_GROUPS).map((feature) => feature.id);
for (const id of [
  "providers",
  "mcp",
  "skills",
  "memory",
  "context",
  "code_intelligence",
  "bots",
  "telemetry",
  "costs",
  "crash_reports",
]) {
  check(`feature list includes ${id}`, featureIds.includes(id));
}
check("feature list is grouped for scanability", DASHBOARD_FEATURE_GROUPS.length >= 6);
check("feature list is broad enough to be the desktop inventory", featureIds.length >= 24);

const signals = buildTelemetryStatus({ telemetry: true, metrics: false });
eq(signals.map((signal) => signal.id), DASHBOARD_TELEMETRY_SIGNALS.map((signal) => signal.id), "telemetry status keeps the canonical signal order");
eq(signals.find((signal) => signal.id === "launch_ping")?.enabled, true, "launch ping follows telemetry setting");
eq(signals.find((signal) => signal.id === "aggregate_metrics")?.enabled, false, "aggregate metrics follows metrics setting");
eq(signals.find((signal) => signal.id === "session_telemetry")?.enabled, true, "local session telemetry remains available without upload");

const metrics = buildDashboardMetrics({
  context: { used: 64_000, window: 128_000, cacheHitTokens: 900, cacheMissTokens: 100, sessionTokens: 144_000 },
  usage: {
    promptTokens: 1_000,
    completionTokens: 200,
    totalTokens: 1_200,
    cacheHitTokens: 80,
    cacheMissTokens: 20,
    sessionCacheHitTokens: 900,
    sessionCacheMissTokens: 100,
    cost: 0.25,
    currency: "$",
    providerStatus: { role: "default", health: "ok", authStatus: "ok", rateLimit: "ok" },
  },
  providerStatus: { role: "default", health: "rate_limited", authStatus: "ok", rateLimit: "cooldown" },
  sessionTokens: 144_000,
  sessionCost: 0.75,
  sessionCurrency: "$",
  sessionTurns: 9,
  turnTokens: 1_200,
  turnCost: 0.25,
});

eq(metrics.contextPct, 50, "context percentage comes from current window");
eq(metrics.cachePct, 90, "cache percentage prefers session aggregate usage");
eq(metrics.providerHealth, "rate_limited", "newer provider status overrides usage snapshot");
eq(metrics.rateLimit, "cooldown", "rate limit comes from current provider status");
eq(metrics.sessionTokens, 144_000, "session tokens are preserved");
eq(metrics.sessionCost, { amount: 0.75, currency: "$" }, "session cost preserves currency");
eq(metrics.turnCost, { amount: 0.25, currency: "$" }, "turn cost preserves currency");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
