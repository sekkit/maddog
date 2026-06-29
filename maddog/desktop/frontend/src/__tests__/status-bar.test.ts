// Run: tsx src/__tests__/status-bar.test.ts

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { StatusBar } from "../components/StatusBar";
import { LocaleProvider } from "../lib/i18n";
import { DEFAULT_STATUS_BAR_ITEMS, STATUS_BAR_ITEM_IDS, normalizeStatusBarItems } from "../lib/statusBarItems";

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

function renderStatusBar(): string {
  return renderToStaticMarkup(
    createElement(
      LocaleProvider,
      null,
      createElement(StatusBar, {
        context: { used: 0, window: 128_000, sessionTokens: 0 },
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
        running: false,
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        turnCost: 0.125,
        cost: 0.125,
        currency: "$",
        modelLabel: "default/gpt-4o-mini",
        items: ["provider", "frontier_budget", "provider_health", "rate_limit", "turn_cost"],
      }),
    ),
  );
}

function renderStatusBarWithoutUsage(): string {
  return renderToStaticMarkup(
    createElement(
      LocaleProvider,
      null,
      createElement(StatusBar, {
        context: { used: 0, window: 128_000, sessionTokens: 0 },
        running: false,
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        modelLabel: "default/gpt-4o-mini",
        items: ["provider", "frontier_budget"],
      }),
    ),
  );
}

function renderStatusBarWithProviderStatusOnly(): string {
  return renderToStaticMarkup(
    createElement(
      LocaleProvider,
      null,
      createElement(StatusBar, {
        context: { used: 0, window: 128_000, sessionTokens: 0 },
        providerStatus: {
          role: "default",
          health: "rate_limited",
          authStatus: "ok",
          rateLimit: "rate_limited",
          lastError: "openai: status 429",
        },
        running: false,
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        modelLabel: "default/gpt-4o-mini",
        items: ["provider_health", "rate_limit"],
      }),
    ),
  );
}

function renderStatusBarWithNewerProviderStatus(): string {
  return renderToStaticMarkup(
    createElement(
      LocaleProvider,
      null,
      createElement(StatusBar, {
        context: { used: 0, window: 128_000, sessionTokens: 0 },
        usage: {
          promptTokens: 100,
          completionTokens: 20,
          totalTokens: 120,
          cacheHitTokens: 80,
          cacheMissTokens: 20,
          sessionCacheHitTokens: 800,
          sessionCacheMissTokens: 200,
          profile: { role: "default", model: "openai/gpt-4o-mini" },
          providerStatus: {
            role: "default",
            health: "ok",
            authStatus: "ok",
            rateLimit: "ok",
          },
        },
        providerStatus: {
          role: "default",
          health: "rate_limited",
          authStatus: "ok",
          rateLimit: "rate_limited",
          lastError: "openai: status 429",
        },
        running: false,
        collaborationMode: "normal",
        toolApprovalMode: "ask",
        modelLabel: "default/gpt-4o-mini",
        items: ["provider_health", "rate_limit"],
      }),
    ),
  );
}

console.log("\nstatus bar provider usage");

const normalized = normalizeStatusBarItems(["provider", "frontier_budget", "provider_health", "rate_limit", "turn_cost"]);
check("provider status item is configurable", normalized.includes("provider"));
check("frontier budget status item is configurable", normalized.includes("frontier_budget"));
check("provider health status item is configurable", normalized.includes("provider_health" as never));
check("rate limit status item is configurable", normalized.includes("rate_limit" as never));
check("all provider status items are known to the editor", STATUS_BAR_ITEM_IDS.includes("provider_health" as never) && STATUS_BAR_ITEM_IDS.includes("rate_limit" as never));
check("default status bar keeps provider usage opt-in", !DEFAULT_STATUS_BAR_ITEMS.includes("provider"));
check("default status bar keeps frontier budget opt-in", !DEFAULT_STATUS_BAR_ITEMS.includes("frontier_budget"));

const html = renderStatusBar();
check("provider item shows usage role", html.includes("frontier"));
check("provider item shows usage model", html.includes("anthropic/claude-3-5-sonnet"));
check("frontier budget item shows remaining and limit", html.includes("3 / 10"));
check("provider health item shows status snapshot", html.includes("data-statusbar-item=\"provider_health\"") && html.includes(">ok<"));
check("rate limit item shows status snapshot", html.includes("data-statusbar-item=\"rate_limit\"") && html.includes(">ok<"));
check("existing turn cost item remains visible", html.includes("$0.1250"));

const emptyHtml = renderStatusBarWithoutUsage();
check("provider item does not invent latest usage before a request", !emptyHtml.includes("default · default/gpt-4o-mini"));
check("provider item shows empty marker without usage profile", emptyHtml.includes("stat__value--empty") && emptyHtml.includes(">-<"));
check("non-frontier budget stays empty when no frontier usage exists", emptyHtml.includes("data-statusbar-item=\"frontier_budget\"") && emptyHtml.includes(">-<"));

const statusOnlyHtml = renderStatusBarWithProviderStatusOnly();
check("provider health item can show failed status without usage tokens", statusOnlyHtml.includes("rate_limited"));
check("rate limit item can show failed status without usage tokens", statusOnlyHtml.includes("data-statusbar-item=\"rate_limit\"") && statusOnlyHtml.includes("rate_limited"));

const newerStatusHtml = renderStatusBarWithNewerProviderStatus();
check("provider health item prefers newer provider status over stale usage status", newerStatusHtml.includes("rate_limited") && !newerStatusHtml.includes(">ok<"));
check("rate limit item prefers newer provider status over stale usage status", newerStatusHtml.includes("data-statusbar-item=\"rate_limit\"") && newerStatusHtml.includes("rate_limited"));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
