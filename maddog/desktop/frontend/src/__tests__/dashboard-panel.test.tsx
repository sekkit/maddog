// Run: tsx src/__tests__/dashboard-panel.test.tsx

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { DashboardPanel } from "../components/DashboardPanel";
import { LocaleProvider } from "../lib/i18n";

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

console.log("\ndashboard panel");

const html = renderToStaticMarkup(
  createElement(
    LocaleProvider,
    null,
    createElement(DashboardPanel, {
      context: { used: 32_000, window: 128_000, sessionTokens: 80_000, cacheHitTokens: 300, cacheMissTokens: 100 },
      usage: {
        promptTokens: 1_000,
        completionTokens: 200,
        totalTokens: 1_200,
        cacheHitTokens: 80,
        cacheMissTokens: 20,
        sessionCacheHitTokens: 300,
        sessionCacheMissTokens: 100,
        cost: 0.125,
        currency: "$",
        providerStatus: { role: "default", health: "ok", authStatus: "ok", rateLimit: "ok" },
      },
      sessionTokens: 80_000,
      sessionCost: 0.5,
      sessionCurrency: "$",
      sessionTurns: 4,
      turnTokens: 1_200,
      turnCost: 0.125,
    }),
  ),
);

function includesAny(values: string[]): boolean {
  return values.some((value) => html.includes(value));
}

check("renders native dashboard title", html.includes("Dashboard"));
check("renders live monitoring metrics", includesAny(["Monitoring", "监测数据", "監測資料"]) && html.includes("25%") && html.includes("75%"));
check("renders analytics section", includesAny(["Analytics", "埋点分析", "埋點分析"]) && includesAny(["Session telemetry sidecar", "会话 telemetry sidecar", "會話 telemetry sidecar"]));
check("renders feature inventory", includesAny(["Feature inventory", "功能点清单", "功能點清單"]) && includesAny(["Code intelligence", "代码智能", "程式碼智能"]) && includesAny(["Native Dashboard", "原生 Dashboard"]));
check("states that local session telemetry is not a web dashboard", includesAny(["not a web dashboard", "不是网页 Dashboard", "不是網頁 Dashboard"]));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
