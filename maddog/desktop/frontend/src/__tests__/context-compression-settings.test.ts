// Run: tsx src/__tests__/context-compression-settings.test.ts

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

const settingsPanel = source("../components/SettingsPanel.tsx");
const types = source("../lib/types.ts");
const en = source("../locales/en.ts");
const zh = source("../locales/zh.ts");

console.log("\ncontext compression settings");

check(
  "settings GUI exposes context compression policy and byte-limit saves",
  includesAll(settingsPanel, [
    "settings.contextCompressionPolicy",
    "settings.contextCompressionThresholdBytes",
    "settings.contextCompressionMaxBytes",
    "setContextCompression({ policy })",
    "setContextCompression({ thresholdBytes: next })",
    "setContextCompression({ maxBytes: next })",
    "SetContextCompression(policy, thresholdBytes, maxBytes)",
  ]),
);

check(
  "settings wire type carries context compression agent fields",
  includesAll(types, [
    'export type ContextCompressionPolicy = "off" | "auto" | "aggressive";',
    "contextCompressionPolicy?: ContextCompressionPolicy;",
    "contextCompressionThresholdBytes?: number;",
    "contextCompressionMaxBytes?: number;",
  ]),
);

check(
  "English and Chinese locales include context compression controls",
  includesAll(en, [
    '"settings.contextCompressionPolicy":',
    '"settings.contextCompressionPolicy.off":',
    '"settings.contextCompressionPolicy.auto":',
    '"settings.contextCompressionPolicy.aggressive":',
    '"settings.contextCompressionThresholdBytes":',
    '"settings.contextCompressionMaxBytes":',
  ]) && includesAll(zh, [
    '"settings.contextCompressionPolicy":',
    '"settings.contextCompressionPolicy.off":',
    '"settings.contextCompressionPolicy.auto":',
    '"settings.contextCompressionPolicy.aggressive":',
    '"settings.contextCompressionThresholdBytes":',
    '"settings.contextCompressionMaxBytes":',
  ]),
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
