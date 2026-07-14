import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { app } from "../lib/bridge";

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
const transcript = source("../components/Transcript.tsx");
const types = source("../lib/types.ts");
const bridge = source("../lib/bridge.ts");
const en = source("../locales/en.ts");
const zh = source("../locales/zh.ts");
const zhTW = source("../locales/zh-TW.ts");

console.log("\nMaddog mechanism GUI contract");

check(
  "settings GUI exposes default, small/background, and frontier model controls",
  includesAll(settingsPanel, [
    "app.SetDefaultModel(ref)",
    "app.RecommendModels()",
    "app.SetSubagentModel(ref)",
    "app.SetAdvisorModel(ref)",
    "app.SetSubagentEffort(e.target.value)",
    "app.SetFrontierRoute(model, enabled, threshold, budget)",
    "settings.subagentModel",
    "settings.advisorModel",
    "settings.frontierRoute",
    "settings.frontierBudget",
  ]),
);

check(
  "settings GUI preserves frontier threshold and budget status labels",
  includesAll(settingsPanel, [
    "settings.frontierThresholdValue",
    "formatBudgetLabel(s.frontierBudget, t)",
    "settings.frontierBudgetUnlimited",
    "settings-model-current--route",
  ]),
);

check(
  "settings GUI exposes one atomic advisor policy mutation and Anthropic-only native controls",
  includesAll(settingsPanel, [
    "return app.SetAdvisorPolicy(maxPerTurn, maxPerSession, nativeEnabled, maxTokens, cacheTTL)",
    "settings.advisorBudgets",
    "settings.advisorMaxPerTurn",
    "settings.advisorMaxPerSession",
    "settings.advisorNative",
    "s.advisorNativeEnabled &&",
    "settings.advisorNativeMaxTokens",
    "settings.advisorNativeCacheTTL",
    '<option value="5m">',
    '<option value="1h">',
  ]) && includesAll(bridge, [
    "SetAdvisorPolicy(maxPerTurn: number, maxPerSession: number, nativeEnabled: boolean, maxTokens: number, cacheTTL: string): Promise<void>",
    "const normalizedMaxTokens = maxTokens === 0 ? 2048 : maxTokens",
    "settings.advisorMaxUsesPerTurn = maxPerTurn",
    "settings.advisorNativeCacheTTL = normalizedCacheTTL",
  ]),
);

check(
  "advisor policy copy is localized and explicitly limited to Anthropic Native",
  [en, zh, zhTW].every((locale) => includesAll(locale, [
    '"settings.advisorBudgets"',
    '"settings.advisorNative"',
    '"settings.advisorNativeHint"',
    '"settings.advisorNativeOptionsHint"',
    '"settings.advisorNativeCacheTTL"',
  ])) && en.includes("only for Anthropic providers"),
);

await app.SetAdvisorPolicy(2, 8, true, 0, "5m");
const advisorPolicy = await app.Settings();
check(
  "bridge mock applies and normalizes the atomic advisor policy",
  advisorPolicy.advisorMaxUsesPerTurn === 2
    && advisorPolicy.advisorMaxUsesPerSession === 8
    && advisorPolicy.advisorNativeEnabled
    && advisorPolicy.advisorNativeMaxTokens === 2048
    && advisorPolicy.advisorNativeCacheTTL === "5m",
);

let invalidAdvisorPolicyRejected = false;
try {
  await app.SetAdvisorPolicy(9, 9, false, 1023, "1h");
} catch {
  invalidAdvisorPolicyRejected = true;
}
const advisorPolicyAfterRejection = await app.Settings();
check(
  "bridge mock rejects invalid advisor policy without a partial mutation",
  invalidAdvisorPolicyRejected
    && advisorPolicyAfterRejection.advisorMaxUsesPerTurn === 2
    && advisorPolicyAfterRejection.advisorMaxUsesPerSession === 8
    && advisorPolicyAfterRejection.advisorNativeEnabled
    && advisorPolicyAfterRejection.advisorNativeMaxTokens === 2048
    && advisorPolicyAfterRejection.advisorNativeCacheTTL === "5m",
);

check(
  "provider editor supports OpenAI, Anthropic, API-key, bearer, and workload identity configuration",
  includesAll(settingsPanel, [
    "export function providerEditorEffectiveKind",
    'kind.trim() || kinds[0] || "openai"',
    'const AUTH_TYPES: readonly string[] = ["api_key", "bearer", "workload_identity"]',
    'authType === "workload_identity"',
    'placeholder="ANTHROPIC_IDENTITY_TOKEN"',
    "authTokenEnv",
    "identityEnv",
  ]) && includesAll(en, [
    '"settings.providerProtocolOpenAI": "OpenAI-compatible"',
    '"settings.authType.apiKey": "API key"',
    '"settings.authType.bearer": "Bearer token"',
    '"settings.authType.workloadIdentity": "Workload identity"',
  ]),
);

check(
  "provider access view displays auth mode, credential env, roles, models, and manual refresh controls",
  includesAll(settingsPanel, [
    "authTypeLabel(group.authType, t)",
    "credentialEnv: providerCredentialEnv(p)",
    "roles: normalizedProviderRoles(p.roles)",
    "existing.roles = uniqueStrings([...existing.roles, ...normalizedProviderRoles(p.roles)])",
    "providerRoleLabel(role, t)",
    "settings.providerRole.advisor",
    "group.credentialEnv || t(\"common.none\")",
    "providerCredentialEnv(editableProvider ?? group.providers[0])",
    "settings.fetchModels",
    "ProviderModelDraftPicker",
    "providerModelCandidates(p.models, fetched)",
    "mergedFetchedProviderModels(p.models, fetched, { preserveCurated: true })",
  ]),
);

check(
  "provider credential env display mirrors backend auth fallback",
  includesAll(settingsPanel, [
    "if (p.credentialEnv) return p.credentialEnv",
    'case "bearer":',
    "return p.authTokenEnv || p.apiKeyEnv",
    'case "workload_identity":',
    "return p.authTokenEnv || p.identityEnv || p.apiKeyEnv",
  ]),
);

check(
  "background provider model refresh is opportunistic and preserves curated model lists",
  includesAll(settingsPanel, [
    "void backgroundApply(async () =>",
    "app.FetchProviderModels(provider)",
    "if (!provider.models || provider.models.length === 0) continue;",
    "mergedFetchedProviderModels(provider.models, fetched, { preserveCurated: true })",
    "app.SaveProvider({ ...provider, models, default: currentDefault })",
    "Background discovery is opportunistic; manual refresh shows errors.",
  ]),
);

check(
  "wire types carry provider auth, official provider templates, small model, and frontier route fields",
  includesAll(types, [
    "export interface ProviderView",
    "authType: string; // api_key|bearer|workload_identity; empty = api_key",
    "authTokenEnv: string;",
    "identityEnv: string;",
    "roles?: string[]; // derived profile roles: default|planner|frontier|small|advisor",
    "export interface SettingsView",
    "subagentModel: string;",
    "advisorModel: string;",
    "advisorMaxUsesPerTurn: number;",
    "advisorMaxUsesPerSession: number;",
    "advisorNativeEnabled: boolean;",
    "advisorNativeMaxTokens: number;",
    "advisorNativeCacheTTL: string;",
    "frontierModel: string;",
    "officialProviders: ProviderView[];",
  ]),
);

check(
  "runtime transcript renders advisor and frontier/skill route events",
  includesAll(transcript, [
    'case "advisor": nodes.push(<AdvisorCard',
    "function AdvisorCard",
    "formatAdvisorRemaining",
    'event === "upgrade"',
    'event === "budget_exceeded"',
    'event === "skill_promoted"',
  ]) && includesAll(en, [
    '"runtimeEvent.upgrade": "Frontier route"',
    '"advisor.kind": "Advisor"',
    '"advisor.turnBudget": "turn {remaining}/{max}"',
  ]),
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
