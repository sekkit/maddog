import { replaceAttachmentRefsForDisplay } from "../lib/attachmentDisplay";
import { invalidateCache, snapshot } from "../lib/composerHistory";
import { getFontFamily } from "../lib/fontFamily";
import { readLegacyLangPref } from "../lib/i18n";
import { loadLayoutSize } from "../lib/layoutPreferences";
import { getTextSize } from "../lib/textSize";
import { readLegacyThemePreference } from "../lib/theme";

let passed = 0;
let failed = 0;

async function check(label: string, fn: () => boolean | Promise<boolean>) {
  try {
    if (await fn()) {
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

console.log("\nMaddog persistence namespace");

await check("attachment display collapses .maddog refs", () => {
  const input = "see @.maddog/attachments/clipboard-20260601-010203.000001.png";
  return replaceAttachmentRefsForDisplay(input) === "see [image]";
});

await check("composer history ignores maddog localStorage", () => {
  resetStorage();
  invalidateCache();
  localStorage.setItem("maddog.composer.history", JSON.stringify([{ text: "from maddog", at: 1 }]));
  return snapshot().then((entries) => !entries.some((entry) => entry.text === "from maddog"));
});

await check("layout preferences read maddog aggregate key", () => {
  resetStorage();
  localStorage.setItem("maddog.layoutPreferences.v1", JSON.stringify({ sizes: { composerHeight: 300 } }));
  return loadLayoutSize("composerHeight", 100) === 300;
});

await check("layout preferences read maddog scalar keys", () => {
  resetStorage();
  localStorage.setItem("maddog.composerHeight", "300");
  return loadLayoutSize("composerHeight", 100) === 300;
});

await check("language migration reads maddog localStorage", () => {
  resetStorage();
  localStorage.setItem("maddog-lang", "zh");
  return readLegacyLangPref() === "zh";
});

await check("theme migration reads maddog localStorage", () => {
  resetStorage();
  localStorage.setItem("maddog-theme", "dark");
  localStorage.setItem("maddog-theme-style", "aurora");
  const pref = readLegacyThemePreference();
  return pref.hasValue && pref.theme === "dark" && pref.style === "aurora";
});

await check("font family reads maddog localStorage", () => {
  resetStorage();
  localStorage.setItem("maddog-font-family", "yahei");
  return getFontFamily() === "yahei";
});

await check("text size reads maddog localStorage", () => {
  resetStorage();
  localStorage.setItem("maddog-text-size", "large");
  return getTextSize() === "large";
});

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
