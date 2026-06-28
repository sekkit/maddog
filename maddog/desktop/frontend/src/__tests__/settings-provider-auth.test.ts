import { app } from "../lib/bridge";
import { classifyProviderModelProbeError } from "../lib/providerModels";
import type { ProviderView } from "../lib/types";

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

function provider(overrides: Partial<ProviderView>): ProviderView {
  return {
    name: "openai-official",
    builtIn: false,
    added: true,
    kind: "openai",
    baseUrl: "https://api.openai.com/v1",
    models: ["gpt-5"],
    modelsUrl: "",
    default: "gpt-5",
    apiKeyEnv: "OLD_OPENAI_API_KEY",
    authType: "official_auth",
    authTokenEnv: "",
    bearerTokenEnv: "OPENAI_ACCESS_TOKEN",
    authHeader: "",
    authScheme: "",
    identityEnv: "",
    identityFile: "",
    federationRuleId: "",
    organizationId: "",
    serviceAccountId: "",
    workspaceId: "",
    officialAuthProfileId: "openai-desktop",
    keySet: true,
    balanceUrl: "",
    contextWindow: 200000,
    reasoningProtocol: "",
    supportedEfforts: [],
    defaultEffort: "",
    roles: ["frontier"],
    roleModels: { frontier: "openai-official/gpt-5" },
    authMode: "official_auth",
    credentialEnv: "OPENAI_ACCESS_TOKEN",
    credentialStatus: "configured",
    gateway: "official_openai",
    frontierEligible: true,
    smallModelEligible: false,
    budgetEligible: true,
    warnings: [],
    ...overrides,
  };
}

console.log("\nsettings provider auth contract");

const official = provider({});
await app.SaveProvider(official);
const settings = await app.Settings();
const saved = settings.providers.find((p) => p.name === "openai-official");
ok("official auth profile id is preserved", saved?.officialAuthProfileId === "openai-desktop");
ok("official auth uses bearer token env as credential env", saved?.credentialEnv === "OPENAI_ACCESS_TOKEN");
ok("settings JSON does not contain token plaintext", !JSON.stringify(settings).includes("sk-official-secret"));

const fetched = await app.FetchProviderModels(provider({
  name: "icodeeasy-frontier",
  baseUrl: "https://gateway.icodeeasy.com/v1",
  authType: "bearer",
  authTokenEnv: "ICODEEASY_TOKEN",
  bearerTokenEnv: "ICODEEASY_TOKEN",
  officialAuthProfileId: "",
  gateway: "icodeeasy",
  credentialEnv: "ICODEEASY_TOKEN",
}));
ok("icodeeasy bearer probe returns chat model candidates", fetched.includes("qwen3-coder"));

ok("auth failure classification is stable", classifyProviderModelProbeError("fetch models: auth_failure: status 401") === "auth_failure");
ok("provider unavailable classification is stable", classifyProviderModelProbeError("fetch models: provider_unavailable: status 500") === "provider_unavailable");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
