export function mergedFetchedProviderModels(current: string[], fetched: string[], options: { preserveCurated?: boolean } = {}): string[] {
  const saved = uniqueStrings(current);
  if (options.preserveCurated && saved.length > 0) return saved;
  return uniqueStrings([...saved, ...fetched]);
}

export function providerModelCandidates(current: string[], fetched: string[]): string[] {
  return uniqueStrings([...current, ...fetched]).filter(isLikelyChatModel);
}

export function providerDefaultModel(currentDefault: string, models: string[]): string {
  return currentDefault && models.includes(currentDefault) ? currentDefault : models[0] ?? "";
}

export function classifyProviderModelProbeError(message: string): "auth_failure" | "provider_unavailable" | "unknown" {
  const lower = String(message ?? "").toLowerCase();
  if (lower.includes("auth_failure") || lower.includes("status 401") || lower.includes("status 403")) return "auth_failure";
  if (
    lower.includes("provider_unavailable") ||
    lower.includes("status 408") ||
    lower.includes("status 429") ||
    lower.includes("status 500") ||
    lower.includes("status 502") ||
    lower.includes("status 503") ||
    lower.includes("status 504") ||
    lower.includes("timeout") ||
    lower.includes("deadline exceeded")
  ) {
    return "provider_unavailable";
  }
  return "unknown";
}

export function isLikelyChatModel(model: string): boolean {
  const lower = model.trim().toLowerCase();
  if (!lower) return false;
  for (const term of ["text-embedding", "text-to-speech", "speech-to-text"]) {
    if (lower.includes(term)) return false;
  }
  const nonChatTokens = new Set([
    "asr",
    "stt",
    "tts",
    "whisper",
    "embedding",
    "moderation",
    "rerank",
    "dall",
    "transcription",
  ]);
  return !lower.split(/[-_./:]+/).some((token) => nonChatTokens.has(token));
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const model = value.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    out.push(model);
  }
  return out;
}
