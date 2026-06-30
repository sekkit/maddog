export const STATUS_BAR_ITEM_IDS = [
  "model",
  "cache",
  "cache_avg",
  "session_tokens",
  "turn_tokens",
  "turn_cost",
  "session_turns",
  "context",
  "compact",
  "cost",
  "balance",
  "provider",
  "frontier_budget",
  "provider_health",
  "rate_limit",
] as const;

export type StatusBarItemId = typeof STATUS_BAR_ITEM_IDS[number];

export const ALL_STATUS_BAR_ITEMS: StatusBarItemId[] = [...STATUS_BAR_ITEM_IDS];

export const DEFAULT_STATUS_BAR_ITEMS: StatusBarItemId[] = [
  "model",
  "cache",
  "cache_avg",
  "session_tokens",
  "turn_tokens",
  "turn_cost",
  "session_turns",
  "context",
  "compact",
  "cost",
  "balance",
];

const statusBarItemSet = new Set<string>(STATUS_BAR_ITEM_IDS);

export function normalizeStatusBarItems(items: readonly string[] | null | undefined): StatusBarItemId[] {
  const out: StatusBarItemId[] = [];
  const seen = new Set<string>();
  for (const raw of items ?? []) {
    const id = String(raw ?? "").trim();
    if (!statusBarItemSet.has(id) || seen.has(id)) continue;
    out.push(id as StatusBarItemId);
    seen.add(id);
  }
  return out.length > 0 ? out : [...DEFAULT_STATUS_BAR_ITEMS];
}
