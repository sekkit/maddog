import { rawToolResultNoteKey } from "../components/ToolCard";
import { ToolCard } from "../components/ToolCard";
import { LocaleProvider } from "../lib/i18n";
import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function eq<T>(a: T, b: T, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntool raw output unavailable note");

eq(rawToolResultNoteKey(true), "tool.rawUnavailable", "missing raw result maps to unavailable note");
eq(rawToolResultNoteKey(false), "", "available raw result has no note");
eq(rawToolResultNoteKey(undefined), "", "legacy full-data response has no note");
ok(Boolean(en["tool.rawUnavailable"]), "English locale includes raw unavailable note");
ok(Boolean(zh["tool.rawUnavailable"]), "Chinese locale includes raw unavailable note");

{
  const item: Extract<Item, { kind: "tool" }> = {
    kind: "tool",
    id: "missing-raw",
    name: "bash",
    args: "{\"command\":\"go test ./...\"}",
    readOnly: false,
    status: "done",
    output: "[compressed tool output]\nraw available at raw://tool/missing-raw",
    rawUnavailable: true,
  };
  const html = renderToStaticMarkup(createElement(LocaleProvider, null, createElement(ToolCard, { item })));
  ok(
    html.includes(en["tool.rawUnavailable"]) || html.includes(zh["tool.rawUnavailable"]),
    "ToolCard renders raw unavailable note",
  );
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
