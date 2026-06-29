// Run: tsx src/__tests__/use-controller-meta.test.ts

import { initialState, reducer, sameMeta, shouldReconcileStaleTurn } from "../lib/useController";
import type { Meta } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function meta(overrides: Partial<Meta> = {}): Meta {
  return {
    label: "DeepSeek-R1",
    ready: true,
    eventChannel: "events",
    cwd: "/repo",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    ...overrides,
  };
}

console.log("\nuse controller meta");

{
  eq(sameMeta(meta(), meta()), true, "identical meta is unchanged");
  eq(sameMeta(meta({ collaborationMode: "normal" }), meta({ collaborationMode: "plan" })), false, "collaboration mode changes invalidate meta equality");
}

{
  const started = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const rendered = reducer(started, { type: "event", e: { kind: "message", text: "done", reasoning: "" } });
  eq(rendered.running, true, "message without turn_done leaves local runtime marked running");
  eq(rendered.turnActive, true, "message without turn_done still belongs to an active turn");
  eq(rendered.live, undefined, "final message closes the live stream before turn_done");
  eq(shouldReconcileStaleTurn(rendered, 1_000, 31_000), true, "stale completed stream still reconciles missed turn_done");
  eq(shouldReconcileStaleTurn(rendered, 1_000, 20_000), false, "fresh completed stream waits before reconciling");
  eq(shouldReconcileStaleTurn({ ...rendered, turnActive: false }, 1_000, 31_000), false, "local pending send before turn_started does not reconcile");
}

{
  const next = reducer(initialState, {
    type: "event",
    e: {
      kind: "usage",
      usage: {
        promptTokens: 100,
        completionTokens: 20,
        totalTokens: 120,
        cacheHitTokens: 80,
        cacheMissTokens: 20,
        sessionCacheHitTokens: 800,
        sessionCacheMissTokens: 200,
        cost: 0.125,
        currency: "$",
        profile: {
          role: "frontier",
          model: "anthropic/claude-3-5-sonnet",
          budgetUsed: 7,
          budgetLimit: 10,
          budgetRemaining: 3,
        },
        providerStatus: {
          role: "frontier",
          health: "ok",
          authStatus: "ok",
          rateLimit: "ok",
        },
      },
    },
  });
  eq(next.usage?.profile?.role, "frontier", "usage reducer preserves provider role");
  eq(next.usage?.profile?.model, "anthropic/claude-3-5-sonnet", "usage reducer preserves provider model");
  eq(next.usage?.profile?.budgetRemaining, 3, "usage reducer preserves frontier budget snapshot");
  eq(next.usage?.providerStatus?.health, "ok", "usage reducer preserves provider health snapshot");
  eq(next.usage?.providerStatus?.rateLimit, "ok", "usage reducer preserves provider rate limit snapshot");
  eq(next.sessionCost, 0.125, "usage reducer still accumulates cost");
}

{
  const next = reducer(initialState, {
    type: "event",
    e: {
      kind: "provider_status",
      providerStatus: {
        role: "default",
        health: "rate_limited",
        authStatus: "ok",
        rateLimit: "rate_limited",
        lastError: "openai: status 429",
      },
    },
  });
  eq(next.providerStatus?.health, "rate_limited", "provider status event preserves health");
  eq(next.providerStatus?.rateLimit, "rate_limited", "provider status event preserves rate limit");
  eq(next.providerStatus?.lastError, "openai: status 429", "provider status event preserves last error");
}

{
  const withUsage = reducer(initialState, {
    type: "event",
    e: {
      kind: "usage",
      usage: {
        promptTokens: 100,
        completionTokens: 20,
        totalTokens: 120,
        cacheHitTokens: 80,
        cacheMissTokens: 20,
        sessionCacheHitTokens: 800,
        sessionCacheMissTokens: 200,
        profile: { role: "default", model: "openai/gpt-4o-mini" },
        providerStatus: { role: "default", health: "ok", authStatus: "ok", rateLimit: "ok" },
      },
    },
  });
  const next = reducer(withUsage, {
    type: "event",
    e: {
      kind: "provider_status",
      providerStatus: {
        role: "default",
        health: "rate_limited",
        authStatus: "ok",
        rateLimit: "rate_limited",
        lastError: "openai: status 429",
      },
    },
  });
  eq(next.usage?.providerStatus?.health, "ok", "usage reducer keeps historical usage status");
  eq(next.providerStatus?.health, "rate_limited", "provider status sequence stores latest status");
  eq(next.providerStatus?.rateLimit, "rate_limited", "provider status sequence stores latest rate limit");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
