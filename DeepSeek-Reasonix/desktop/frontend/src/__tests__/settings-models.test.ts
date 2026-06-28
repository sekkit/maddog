import type { ProviderView, SettingsView } from "../lib/types";

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

console.log("\nsettings provider profiles");

const frontier: ProviderView = {
  name: "anthropic-frontier",
  builtIn: false,
  added: true,
  kind: "anthropic",
  baseUrl: "https://api.anthropic.com",
  models: ["claude-sonnet-4"],
  modelsUrl: "",
  default: "claude-sonnet-4",
  apiKeyEnv: "ANTHROPIC_FRONTIER_KEY",
  authType: "api_key",
  authTokenEnv: "",
  bearerTokenEnv: "",
  authHeader: "",
  authScheme: "",
  identityEnv: "",
  identityFile: "",
  federationRuleId: "",
  organizationId: "",
  serviceAccountId: "",
  workspaceId: "",
  officialAuthProfileId: "",
  keySet: true,
  balanceUrl: "",
  contextWindow: 200000,
  reasoningProtocol: "",
  supportedEfforts: [],
  defaultEffort: "",
  roles: ["frontier", "advisor", "checker"],
  roleModels: {
    frontier: "anthropic-frontier/claude-sonnet-4",
    advisor: "anthropic-frontier/claude-sonnet-4",
    checker: "anthropic-frontier/claude-sonnet-4",
  },
  authMode: "api_key",
  credentialEnv: "ANTHROPIC_FRONTIER_KEY",
  credentialStatus: "configured",
  gateway: "official_anthropic",
  frontierEligible: true,
  smallModelEligible: false,
  budgetEligible: true,
  warnings: [],
};

const settings = {
  providers: [frontier],
  providerProfileWarnings: [],
} as unknown as SettingsView;

ok("provider exposes frontier role", settings.providers[0].roles.includes("frontier"));
ok("provider exposes role model mapping", settings.providers[0].roleModels.frontier === "anthropic-frontier/claude-sonnet-4");
ok("provider exposes auth and credential status", settings.providers[0].authMode === "api_key" && settings.providers[0].credentialStatus === "configured");
ok("provider exposes gateway and budget eligibility", settings.providers[0].gateway === "official_anthropic" && settings.providers[0].budgetEligible);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
