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
        profile: {
          role: "default",
          model: "deepseek-v4-flash",
          budgetUsed: 50_000,
          budgetLimit: 500_000,
          budgetRemaining: 450_000,
        },
      },
      sessionTokens: 80_000,
      sessionCost: 0.5,
      sessionCurrency: "$",
      sessionTurns: 4,
      turnTokens: 1_200,
      turnCost: 0.125,
      modelLabel: "deepseek-v4-flash",
      balance: { available: true, display: "¥46.52" },
      settingsView: {
        telemetry: true,
        metrics: true,
        defaultModel: "deepseek-v4-flash",
        upgradeEnabled: true,
        frontierModel: "deepseek/deepseek-v4-pro",
        upgradeThreshold: 3,
        frontierBudget: 500_000,
        agent: {
          contextCompressionPolicy: "auto",
          contextCompressionThresholdBytes: 8_192,
          contextCompressionMaxBytes: 4_096,
        },
      },
      contextInfo: {
        usedTokens: 32_000,
        windowTokens: 128_000,
        promptTokens: 5_000,
        completionTokens: 900,
        totalTokens: 5_900,
        reasoningTokens: 120,
        cacheHitTokens: 4_000,
        cacheMissTokens: 1_000,
        sessionCacheHitTokens: 9_000,
        sessionCacheMissTokens: 1_000,
        sessionCompletionTokens: 1_800,
        requestCount: 6,
        elapsedMs: 125_000,
        sessionCost: 0.75,
        sessionCurrency: "$",
        compressionEvents: 2,
        compressionRawChars: 120_000,
        compressionCompressedChars: 50_000,
        compressionSavedChars: 70_000,
        compressionRawTokens: 10_000,
        compressionCompressedTokens: 4_200,
        compressionSavedTokens: 5_800,
        sources: {
          executor: {
            promptTokens: 4_000,
            completionTokens: 700,
            totalTokens: 4_700,
            reasoningTokens: 100,
            cacheHitTokens: 3_200,
            cacheMissTokens: 800,
            requestCount: 4,
            sessionCost: 0.45,
            sessionCurrency: "$",
          },
          planner: {
            promptTokens: 1_000,
            completionTokens: 200,
            totalTokens: 1_200,
            reasoningTokens: 20,
            cacheHitTokens: 800,
            cacheMissTokens: 200,
            requestCount: 2,
            sessionCost: 0.3,
            sessionCurrency: "$",
          },
        },
        readFiles: [
          { path: "src/App.tsx", turn: 1, time: 1_000 },
          { path: "src/components/DashboardPanel.tsx", turn: 1, time: 2_000 },
        ],
        changedFiles: [{ path: "src/components/DashboardPanel.tsx", sources: ["edit"], turns: [1], latestTime: 3_000 }],
      },
      historyPage: {
        messages: [
          { role: "user", content: "检查 dashboard 的数据，尤其是 frontier 和日志", submitText: "检查 dashboard 的数据，尤其是 frontier 和日志", checkpointTurn: 1, createdAt: 1_800_000_000_123 },
          { role: "assistant", content: "我会确认本地会话数据并补齐 Dashboard 日志。", checkpointTurn: 1, createdAt: 1_800_000_001_000, toolCalls: [{ id: "tool-1", name: "ContextPanel", arguments: "{}", summary: "loaded context" }] },
        ],
        startTurn: 1,
        endTurn: 1,
        totalTurns: 1,
        hasOlder: false,
      },
    }),
  ),
);

function includesAny(values: string[]): boolean {
  return values.some((value) => html.includes(value));
}

function hasDetailValue(markup: string, labels: string[], value: string): boolean {
  return labels.some((label) => markup.includes(`<span>${label}</span><strong>${value}</strong>`));
}

function hasMetricValue(markup: string, labels: string[], value: string): boolean {
  return labels.some((label) => markup.includes(`${label}</span><strong>${value}</strong>`));
}

check("renders native dashboard title", html.includes("Dashboard"));
check("renders live monitoring metrics", includesAny(["Monitoring", "监测数据", "監測資料"]) && html.includes("25%") && html.includes("75%"));
check("renders analytics section", includesAny(["Analytics", "埋点分析", "埋點分析"]) && includesAny(["Session telemetry sidecar", "会话 telemetry sidecar", "會話 telemetry sidecar"]));
check("renders concrete token detail", includesAny(["Token detail", "Token 明细", "Token 明細"]) && html.includes("5,000") && html.includes("900") && html.includes("120"));
check("renders concrete cache detail", includesAny(["Cache detail", "缓存明细", "快取明細"]) && html.includes("4,000") && html.includes("1,000") && html.includes("80.00%") && html.includes("90.00%"));
check("renders provider, balance, and frontier route detail", html.includes("deepseek-v4-flash") && html.includes("¥46.52") && html.includes("deepseek/deepseek-v4-pro") && html.includes("500,000") && html.includes("50,000 / 500,000"));
check("renders runtime activity detail", includesAny(["Runtime activity", "运行活动", "執行活動"]) && html.includes("6") && includesAny(["2m 5s", "2分5秒"]) && html.includes("2") && html.includes("1"));
check("renders compression and source breakdown", includesAny(["Compression detail", "压缩明细", "壓縮明細"]) && html.includes("5.8k") && html.includes("8,192 bytes") && html.includes("4,096 bytes") && includesAny(["Model usage breakdown", "模型用量拆分"]));
check("renders session log with user request and tool activity", includesAny(["Session log", "会话日志", "會話日誌"]) && includesAny(["User request", "用户请求", "使用者請求"]) && html.includes("frontier") && includesAny(["Tool calls", "工具调用", "工具呼叫"]));
check("renders session log as a dynamic scroll stream", html.includes("dashboard-panel__log-list") && html.includes("dashboard-panel__log-meta") && html.includes("2027-01-15T08:00:00.123Z"));
check("renders session log model token and request chips", includesAny(["Model deepseek-v4-flash", "模型 deepseek-v4-flash"]) && includesAny(["Tokens 5,900", "Token 5,900"]) && includesAny(["Requests 6", "请求 6", "請求 6"]));
check("renders feature inventory", includesAny(["Feature inventory", "功能点清单", "功能點清單"]) && includesAny(["Code intelligence", "代码智能", "程式碼智能"]) && includesAny(["Native Dashboard", "原生 Dashboard"]));
check("states that local session telemetry is not a web dashboard", includesAny(["not a web dashboard", "不是网页 Dashboard", "不是網頁 Dashboard"]));

const idleCompressionHtml = renderToStaticMarkup(
  createElement(
    LocaleProvider,
    null,
    createElement(DashboardPanel, {
      context: { used: 0, window: 128_000 },
      settingsView: {
        telemetry: true,
        metrics: true,
        defaultModel: "deepseek-v4-flash",
        upgradeEnabled: true,
        frontierModel: "deepseek/deepseek-v4-pro",
        upgradeThreshold: 3,
        frontierBudget: 500_000,
        agent: {
          contextCompressionPolicy: "auto",
          contextCompressionThresholdBytes: 8_192,
          contextCompressionMaxBytes: 4_096,
        },
      },
      contextInfo: {
        usedTokens: 0,
        windowTokens: 128_000,
        compressionEvents: 0,
      },
    }),
  ),
);

check(
  "renders zero compression details instead of dashes before first compression",
  hasDetailValue(idleCompressionHtml, ["Events", "事件"], "0") &&
    hasDetailValue(idleCompressionHtml, ["Raw tokens", "原始 tokens"], "0") &&
    hasDetailValue(idleCompressionHtml, ["Compressed tokens", "压缩后 tokens", "壓縮後 tokens"], "0") &&
    hasDetailValue(idleCompressionHtml, ["Saved tokens", "节省 tokens", "節省 tokens"], "0") &&
    hasDetailValue(idleCompressionHtml, ["Raw chars", "原始字符", "原始字元"], "0") &&
    hasDetailValue(idleCompressionHtml, ["Saved chars", "节省字符", "節省字元"], "0"),
);

const restoredSessionHtml = renderToStaticMarkup(
  createElement(
    LocaleProvider,
    null,
    createElement(DashboardPanel, {
      context: { used: 0, window: 128_000 },
      settingsView: {
        telemetry: true,
        metrics: true,
        defaultModel: "maddog-default-model",
        upgradeEnabled: true,
        frontierModel: "deepseek/deepseek-v4-pro",
        upgradeThreshold: 3,
        frontierBudget: 500_000,
      },
    }),
  ),
);

check(
  "renders provider metric from the configured model when live health is absent",
  hasMetricValue(restoredSessionHtml, ["Provider"], "maddog-default-model"),
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
