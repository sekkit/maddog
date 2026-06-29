// Run: tsx src/__tests__/capabilities-code-intelligence.test.ts

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

console.log("\ncapabilities code intelligence");

check(
  "wire types carry code intelligence backend status and tool mappings",
  includesAll(types, [
    "export interface CodeIntelligenceBackendView",
    'status: "ready" | "unknown" | "degraded" | "disabled" | "invalid" | string;',
    'indexStatus?: "initialized" | "not_initialized" | string;',
    "capabilities: CodeIntelligenceBackendCapabilities;",
    "toolMapping?: Record<string, string>;",
    "benchmark?: CodeIntelligenceBenchmarkView;",
    "benchmarkRunning?: boolean;",
    "export interface CodeIntelligenceBenchmarkBackendView",
    "error?: string;",
    "backends?: CodeIntelligenceBenchmarkBackendView[];",
    "codeIntelligenceBackends?: CodeIntelligenceBackendView[];",
  ]),
);

check(
  "bridge exposes code intelligence management actions and mock benchmark state",
  includesAll(bridge, [
    "capCodeIntelligenceBackends",
    "setCodeIntelligenceBackendEnabled",
    "retryCodeIntelligenceBackend",
    "runCodeIntelligenceBenchmark",
    'id: "codegraph"',
    'kind: "builtin"',
    'indexStatus: "not_initialized"',
    "benchmarkRunning: false",
    "backends: [{ id: \"codegraph\"",
    'jsonPath: "',
    'context_pack: "mcp__codegraph__context"',
    "codeIntelligenceBackends: capCodeIntelligenceBackends.map",
  ]),
);

check(
  "capabilities panel renders code intelligence health, freshness, capabilities, and tool counts",
  includesAll(panel, [
    "CodeIntelligenceSection",
    "codeIntelligenceBackends={view.codeIntelligenceBackends ?? []}",
    "key={codeIntelBackendKey(backend, index)}",
    "caps.codeIntelligence",
    "caps.codeIntelligenceSummary",
    "codeIntelStatusLabel(backend.status, t)",
    "codeIntelIndexStatusLabel(backend.indexStatus, t)",
    "codeIntelCapabilityLabels(backend.capabilities, t)",
    "caps.codeIntelligenceTools",
    "app.setCodeIntelligenceBackendEnabled",
    "app.retryCodeIntelligenceBackend",
    "app.runCodeIntelligenceBenchmark",
    "caps.codeIntelligenceEnable",
    "caps.codeIntelligenceRetry",
    "caps.codeIntelligenceBenchmark",
    "caps.codeIntelligenceBenchmarkRunning",
    "caps.codeIntelligenceBenchmarkError",
  ]),
);

check(
  "English and Chinese locales include code intelligence labels",
  includesAll(en, [
    '"caps.codeIntelligence":',
    '"caps.codeIntelligenceSummary":',
    '"caps.codeIntelligenceStatus.ready":',
    '"caps.codeIntelligenceStatus.degraded":',
    '"caps.codeIntelligenceIndexStatus":',
    '"caps.codeIntelligenceTools":',
    '"caps.codeIntelligenceBenchmark":',
    '"caps.codeIntelligenceRetry":',
    '"caps.codeIntelligenceEnable":',
    '"caps.codeIntelligenceBenchmarkError":',
  ]) && includesAll(zh, [
    '"caps.codeIntelligence":',
    '"caps.codeIntelligenceSummary":',
    '"caps.codeIntelligenceStatus.ready":',
    '"caps.codeIntelligenceStatus.degraded":',
    '"caps.codeIntelligenceIndexStatus":',
    '"caps.codeIntelligenceTools":',
    '"caps.codeIntelligenceBenchmark":',
    '"caps.codeIntelligenceRetry":',
    '"caps.codeIntelligenceEnable":',
    '"caps.codeIntelligenceBenchmarkError":',
  ]),
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
