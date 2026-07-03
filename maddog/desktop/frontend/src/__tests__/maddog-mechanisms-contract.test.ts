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
const transcript = source("../components/Transcript.tsx");
const types = source("../lib/types.ts");
const en = source("../locales/en.ts");

console.log("\nMaddog mechanism GUI contract");

check(
  "settings GUI exposes default, small/background, and frontier model controls",
  includesAll(settingsPanel, [
    "app.SetDefaultModel(ref)",
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
