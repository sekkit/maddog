import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { replaceAttachmentRefsForDisplay } from "../lib/attachmentDisplay";
import { snapshot } from "../lib/composerHistory";
import { getFontFamily } from "../lib/fontFamily";
import { readLegacyLangPref } from "../lib/i18n";
import { loadLayoutSize } from "../lib/layoutPreferences";
import { getTextSize } from "../lib/textSize";

let passed = 0;
let failed = 0;

function check(label: string, fn: () => boolean) {
  try {
    if (fn()) {
      process.stdout.write(`  PASS  ${label}\n`);
      passed += 1;
    } else {
      process.stdout.write(`  FAIL  ${label}\n`);
      failed += 1;
    }
  } catch (e) {
    process.stdout.write(`  ERROR ${label}: ${(e as Error).message}\n`);
    failed += 1;
  }
}

class MemoryStorage {
  private values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }

  clear(): void {
    this.values.clear();
  }
}

const storage = new MemoryStorage();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: storage,
});
Object.defineProperty(globalThis, "window", {
  configurable: true,
  value: { localStorage: storage },
});
Object.defineProperty(globalThis, "navigator", {
  configurable: true,
  value: { language: "en-US" },
});

function resetStorage(): void {
  storage.clear();
}

console.log("\nMaddog isolation");

check("attachment display ignores .reasonix refs", () => {
  const input = "see @.reasonix/attachments/clipboard-20260601-010203.000001.png";
  return replaceAttachmentRefsForDisplay(input) === input;
});

check("composer history ignores reasonix localStorage", () => {
  resetStorage();
  localStorage.setItem("reasonix.composer.history", JSON.stringify([{ text: "from reasonix", at: 1 }]));
  return snapshot().length === 0;
});

check("layout preferences ignore reasonix aggregate key", () => {
  resetStorage();
  localStorage.setItem("reasonix.layoutPreferences.v1", JSON.stringify({ sizes: { composerHeight: 300 } }));
  return loadLayoutSize("composerHeight", 100) === 100;
});

check("layout preferences ignore reasonix scalar keys", () => {
  resetStorage();
  localStorage.setItem("reasonix.composerHeight", "300");
  return loadLayoutSize("composerHeight", 100) === 100;
});

check("language migration ignores reasonix localStorage", () => {
  resetStorage();
  localStorage.setItem("reasonix-lang", "zh");
  return readLegacyLangPref() === "";
});

check("theme migration ignores reasonix localStorage", () => {
  const themeSource = readFileSync(fileURLToPath(new URL("../lib/theme.ts", import.meta.url)), "utf8");
  return themeSource.includes('"maddog-theme"') && !themeSource.includes("reasonix-theme");
});

check("font family ignores reasonix localStorage", () => {
  resetStorage();
  localStorage.setItem("reasonix-font-family", "yahei");
  return getFontFamily() === "system";
});

check("text size ignores reasonix localStorage", () => {
  resetStorage();
  localStorage.setItem("reasonix-text-size", "large");
  return getTextSize() === "default";
});

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
