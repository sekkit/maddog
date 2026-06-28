import { app } from "../lib/bridge";
import type { CodeBackendBenchmarkView } from "../lib/types";

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

console.log("\ncode backend capabilities contract");

const before = await app.Capabilities();
const builtIn = before.codeBackends.find((backend) => backend.id === "builtin-codegraph");
ok("capabilities exposes code backend list", Boolean(builtIn));
ok("code backend exposes capability and risk labels", Boolean(builtIn?.capabilities.includes("symbol_search") && builtIn?.risks.includes("read")));

const summary: CodeBackendBenchmarkView = await app.RunCodeBackendBenchmark("builtin-codegraph");
ok("benchmark binding returns summary", summary.backendId === "builtin-codegraph" && summary.path.length > 0);
ok("benchmark summary contains comparable metrics", summary.tokenCharsReturned >= 0 && summary.toolFailures >= 0);
ok("benchmark summary contains citation precision", typeof summary.citationPrecision === "number" && summary.citationPrecision >= 0);

const afterBench = await app.Capabilities();
const benchmarked = afterBench.codeBackends.find((backend) => backend.id === "builtin-codegraph");
ok("capabilities surfaces latest benchmark summary", Boolean(benchmarked?.benchmark?.path && typeof benchmarked.benchmark.citationPrecision === "number"));

await app.SetCodeBackendEnabled("serena", true);
await app.RetryCodeBackendHealth("serena");
const afterToggle = await app.Capabilities();
const external = afterToggle.codeBackends.find((backend) => backend.id === "serena");
ok("external backend can be toggled and retried through bridge", Boolean(external?.enabled));

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
