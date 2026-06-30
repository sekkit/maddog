import type { ContextInfo, WireProviderStatus, WireUsage } from "./types";

export type DashboardFeatureTone = "ready" | "watch" | "local";

export interface DashboardFeature {
  id: string;
  titleKey: string;
  detailKey: string;
  tone?: DashboardFeatureTone;
}

export interface DashboardFeatureGroup {
  id: string;
  titleKey: string;
  summaryKey: string;
  features: DashboardFeature[];
}

export interface DashboardTelemetrySignal {
  id: string;
  titleKey: string;
  detailKey: string;
  privacyKey: string;
  source: "remote" | "local";
  defaultEnabled: boolean;
}

export interface DashboardTelemetryStatus extends DashboardTelemetrySignal {
  enabled: boolean;
}

export interface DashboardMoneyMetric {
  amount: number;
  currency?: string;
}

export interface DashboardMetrics {
  contextPct: number | null;
  cachePct: number | null;
  sessionTokens: number;
  turnTokens: number;
  sessionTurns?: number;
  sessionCost: DashboardMoneyMetric;
  turnCost: DashboardMoneyMetric;
  providerHealth: string;
  rateLimit: string;
}

export const DASHBOARD_FEATURE_GROUPS: DashboardFeatureGroup[] = [
  {
    id: "workflow",
    titleKey: "dashboard.group.workflow",
    summaryKey: "dashboard.group.workflowSummary",
    features: [
      { id: "chat", titleKey: "dashboard.feature.chat", detailKey: "dashboard.feature.chatDetail" },
      { id: "transcript", titleKey: "dashboard.feature.transcript", detailKey: "dashboard.feature.transcriptDetail" },
      { id: "slash_commands", titleKey: "dashboard.feature.slashCommands", detailKey: "dashboard.feature.slashCommandsDetail" },
      { id: "references", titleKey: "dashboard.feature.references", detailKey: "dashboard.feature.referencesDetail" },
      { id: "rewind", titleKey: "dashboard.feature.rewind", detailKey: "dashboard.feature.rewindDetail" },
    ],
  },
  {
    id: "models",
    titleKey: "dashboard.group.models",
    summaryKey: "dashboard.group.modelsSummary",
    features: [
      { id: "providers", titleKey: "dashboard.feature.providers", detailKey: "dashboard.feature.providersDetail" },
      { id: "models", titleKey: "dashboard.feature.models", detailKey: "dashboard.feature.modelsDetail" },
      { id: "provider_profiles", titleKey: "dashboard.feature.providerProfiles", detailKey: "dashboard.feature.providerProfilesDetail" },
      { id: "frontier_budget", titleKey: "dashboard.feature.frontierBudget", detailKey: "dashboard.feature.frontierBudgetDetail" },
      { id: "balances", titleKey: "dashboard.feature.balances", detailKey: "dashboard.feature.balancesDetail" },
    ],
  },
  {
    id: "capabilities",
    titleKey: "dashboard.group.capabilities",
    summaryKey: "dashboard.group.capabilitiesSummary",
    features: [
      { id: "mcp", titleKey: "dashboard.feature.mcp", detailKey: "dashboard.feature.mcpDetail" },
      { id: "tools", titleKey: "dashboard.feature.tools", detailKey: "dashboard.feature.toolsDetail" },
      { id: "skills", titleKey: "dashboard.feature.skills", detailKey: "dashboard.feature.skillsDetail" },
      { id: "code_intelligence", titleKey: "dashboard.feature.codeIntelligence", detailKey: "dashboard.feature.codeIntelligenceDetail" },
      { id: "permissions", titleKey: "dashboard.feature.permissions", detailKey: "dashboard.feature.permissionsDetail" },
    ],
  },
  {
    id: "memory_context",
    titleKey: "dashboard.group.memoryContext",
    summaryKey: "dashboard.group.memoryContextSummary",
    features: [
      { id: "memory", titleKey: "dashboard.feature.memory", detailKey: "dashboard.feature.memoryDetail" },
      { id: "context", titleKey: "dashboard.feature.context", detailKey: "dashboard.feature.contextDetail" },
      { id: "compression", titleKey: "dashboard.feature.compression", detailKey: "dashboard.feature.compressionDetail" },
      { id: "cache", titleKey: "dashboard.feature.cache", detailKey: "dashboard.feature.cacheDetail" },
      { id: "costs", titleKey: "dashboard.feature.costs", detailKey: "dashboard.feature.costsDetail" },
    ],
  },
  {
    id: "desktop",
    titleKey: "dashboard.group.desktop",
    summaryKey: "dashboard.group.desktopSummary",
    features: [
      { id: "projects", titleKey: "dashboard.feature.projects", detailKey: "dashboard.feature.projectsDetail" },
      { id: "workspace_files", titleKey: "dashboard.feature.workspaceFiles", detailKey: "dashboard.feature.workspaceFilesDetail" },
      { id: "settings", titleKey: "dashboard.feature.settings", detailKey: "dashboard.feature.settingsDetail" },
      { id: "hooks", titleKey: "dashboard.feature.hooks", detailKey: "dashboard.feature.hooksDetail" },
      { id: "shortcuts", titleKey: "dashboard.feature.shortcuts", detailKey: "dashboard.feature.shortcutsDetail" },
    ],
  },
  {
    id: "integrations",
    titleKey: "dashboard.group.integrations",
    summaryKey: "dashboard.group.integrationsSummary",
    features: [
      { id: "bots", titleKey: "dashboard.feature.bots", detailKey: "dashboard.feature.botsDetail" },
      { id: "qq_bot", titleKey: "dashboard.feature.qqBot", detailKey: "dashboard.feature.qqBotDetail" },
      { id: "feishu_bot", titleKey: "dashboard.feature.feishuBot", detailKey: "dashboard.feature.feishuBotDetail" },
      { id: "weixin_bot", titleKey: "dashboard.feature.weixinBot", detailKey: "dashboard.feature.weixinBotDetail" },
    ],
  },
  {
    id: "observability",
    titleKey: "dashboard.group.observability",
    summaryKey: "dashboard.group.observabilitySummary",
    features: [
      { id: "telemetry", titleKey: "dashboard.feature.telemetry", detailKey: "dashboard.feature.telemetryDetail", tone: "watch" },
      { id: "metrics", titleKey: "dashboard.feature.metrics", detailKey: "dashboard.feature.metricsDetail", tone: "watch" },
      { id: "crash_reports", titleKey: "dashboard.feature.crashReports", detailKey: "dashboard.feature.crashReportsDetail", tone: "watch" },
      { id: "status_bar", titleKey: "dashboard.feature.statusBar", detailKey: "dashboard.feature.statusBarDetail" },
      { id: "dashboard", titleKey: "dashboard.feature.dashboard", detailKey: "dashboard.feature.dashboardDetail", tone: "local" },
    ],
  },
];

export const DASHBOARD_TELEMETRY_SIGNALS: DashboardTelemetrySignal[] = [
  {
    id: "launch_ping",
    titleKey: "dashboard.telemetry.launch",
    detailKey: "dashboard.telemetry.launchDetail",
    privacyKey: "dashboard.telemetry.launchPrivacy",
    source: "remote",
    defaultEnabled: true,
  },
  {
    id: "aggregate_metrics",
    titleKey: "dashboard.telemetry.aggregate",
    detailKey: "dashboard.telemetry.aggregateDetail",
    privacyKey: "dashboard.telemetry.aggregatePrivacy",
    source: "remote",
    defaultEnabled: true,
  },
  {
    id: "crash_performance",
    titleKey: "dashboard.telemetry.crash",
    detailKey: "dashboard.telemetry.crashDetail",
    privacyKey: "dashboard.telemetry.crashPrivacy",
    source: "remote",
    defaultEnabled: true,
  },
  {
    id: "session_telemetry",
    titleKey: "dashboard.telemetry.session",
    detailKey: "dashboard.telemetry.sessionDetail",
    privacyKey: "dashboard.telemetry.sessionPrivacy",
    source: "local",
    defaultEnabled: true,
  },
];

export function flattenDashboardFeatures(groups: readonly DashboardFeatureGroup[]): DashboardFeature[] {
  return groups.flatMap((group) => group.features);
}

export function buildTelemetryStatus(settings?: { telemetry?: boolean; metrics?: boolean }): DashboardTelemetryStatus[] {
  const telemetry = settings?.telemetry !== false;
  const metrics = settings?.metrics !== false;
  return DASHBOARD_TELEMETRY_SIGNALS.map((signal) => {
    let enabled = signal.defaultEnabled;
    if (signal.id === "launch_ping" || signal.id === "crash_performance") enabled = telemetry;
    if (signal.id === "aggregate_metrics") enabled = metrics;
    if (signal.id === "session_telemetry") enabled = true;
    return { ...signal, enabled };
  });
}

function safePct(numerator: number, denominator: number): number | null {
  if (denominator <= 0) return null;
  return Math.max(0, Math.min(100, Math.round((numerator / denominator) * 100)));
}

function cachePct(context: ContextInfo, usage?: WireUsage): number | null {
  const sessionHit = usage?.sessionCacheHitTokens ?? 0;
  const sessionMiss = usage?.sessionCacheMissTokens ?? 0;
  if (sessionHit + sessionMiss > 0) return safePct(sessionHit, sessionHit + sessionMiss);
  const turnHit = usage?.cacheHitTokens ?? 0;
  const turnMiss = usage?.cacheMissTokens ?? 0;
  if (turnHit + turnMiss > 0) return safePct(turnHit, turnHit + turnMiss);
  const contextHit = context.cacheHitTokens ?? 0;
  const contextMiss = context.cacheMissTokens ?? 0;
  return safePct(contextHit, contextHit + contextMiss);
}

function currentProviderStatus(usage: WireUsage | undefined, providerStatus: WireProviderStatus | undefined): WireProviderStatus | undefined {
  return providerStatus ?? usage?.providerStatus;
}

function money(amount?: number, currency?: string, fallbackCurrency?: string): DashboardMoneyMetric {
  return { amount: typeof amount === "number" && Number.isFinite(amount) ? amount : 0, currency: currency || fallbackCurrency };
}

export function buildDashboardMetrics({
  context,
  usage,
  providerStatus,
  sessionTokens,
  sessionCost,
  sessionCurrency,
  sessionTurns,
  turnTokens,
  turnCost,
}: {
  context: ContextInfo;
  usage?: WireUsage;
  providerStatus?: WireProviderStatus;
  sessionTokens?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  sessionTurns?: number;
  turnTokens?: number;
  turnCost?: number;
}): DashboardMetrics {
  const status = currentProviderStatus(usage, providerStatus);
  const currency = sessionCurrency || usage?.currency;
  return {
    contextPct: safePct(context.used, context.window),
    cachePct: cachePct(context, usage),
    sessionTokens: Math.max(0, Math.round(sessionTokens ?? context.sessionTokens ?? usage?.totalTokens ?? 0)),
    turnTokens: Math.max(0, Math.round(turnTokens ?? usage?.totalTokens ?? 0)),
    sessionTurns,
    sessionCost: money(sessionCost ?? context.sessionCost ?? usage?.cost ?? usage?.costUsd, sessionCurrency ?? context.sessionCurrency ?? usage?.currency),
    turnCost: money(turnCost ?? usage?.cost ?? usage?.costUsd, usage?.currency, currency),
    providerHealth: status?.health?.trim() || (usage?.profile ? "ok" : "-"),
    rateLimit: status?.rateLimit?.trim() || "-",
  };
}
