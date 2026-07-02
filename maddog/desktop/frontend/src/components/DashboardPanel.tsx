import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Activity, BarChart3, CheckCircle2, CircleDollarSign, EyeOff, FileText, ListChecks, MessageSquare, RadioTower, Server, Settings, ShieldCheck, Signal, UserRound, WifiOff, Wrench, Zap } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import {
  DASHBOARD_FEATURE_GROUPS,
  buildDashboardMetrics,
  buildTelemetryStatus,
  flattenDashboardFeatures,
  type DashboardTelemetryStatus,
} from "../lib/dashboardData";
import { useI18n, type DictKey, type Translator } from "../lib/i18n";
import { formatMoneyLocalized } from "../lib/money";
import type { BalanceInfo, ContextInfo, ContextPanelInfo, HistoryMessage, HistoryPage, ReadFileRecord, SettingsView, WireProfile, WireProviderStatus, WireUsage } from "../lib/types";
import { contextCompressionBreakdown, contextSourceRows, formatCacheHitRate, formatMetricTokens, type ContextSourceRow } from "./ContextPanel";

type DashboardSettingsView = Pick<
  SettingsView,
  "telemetry" | "metrics" | "defaultModel" | "upgradeEnabled" | "frontierModel" | "upgradeThreshold" | "frontierBudget"
> & {
  agent?: Pick<SettingsView["agent"], "contextCompressionPolicy" | "contextCompressionThresholdBytes" | "contextCompressionMaxBytes">;
};

interface DashboardPanelProps {
  tabId?: string;
  context: ContextInfo;
  usage?: WireUsage;
  providerStatus?: WireProviderStatus;
  contextInfo?: ContextPanelInfo | null;
  historyPage?: HistoryPage | null;
  settingsView?: DashboardSettingsView | null;
  balance?: BalanceInfo;
  modelLabel?: string;
  sessionTokens?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  sessionTurns?: number;
  turnTokens?: number;
  turnCost?: number;
  refreshKey?: number;
  onOpenSettings?: () => void;
}

interface DashboardLogEntry {
  key: string;
  tone: "user" | "assistant" | "tool" | "system" | "data";
  title: string;
  body: string;
  time?: number;
  timeLabel: string;
  timeIso?: string;
  chips: string[];
  sequence: number;
}

function tk(key: string): DictKey {
  return key as DictKey;
}

function fmtNumber(value: number | undefined): string {
  if (typeof value !== "number" || value <= 0) return "-";
  return value.toLocaleString();
}

function fmtAnyNumber(value: number | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "-";
  return Math.max(0, Math.round(value)).toLocaleString();
}

function fmtZeroNumber(value: number | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "0";
  return Math.max(0, Math.round(value)).toLocaleString();
}

function fmtPct(value: number | null): string {
  return value === null ? "-" : `${value}%`;
}

function fmtTurns(turns: number | undefined, t: Translator): string {
  if (typeof turns !== "number" || turns < 0) return "-";
  return t(turns === 1 ? "history.turnOne" : "history.turnOther", { n: turns });
}

function enabledLabel(signal: DashboardTelemetryStatus, t: Translator): string {
  if (signal.source === "local") return t("dashboard.telemetry.local");
  return signal.enabled ? t("dashboard.telemetry.enabled") : t("dashboard.telemetry.disabled");
}

function signalIcon(signal: DashboardTelemetryStatus) {
  if (signal.source === "local") return <ShieldCheck size={14} />;
  return signal.enabled ? <RadioTower size={14} /> : <WifiOff size={14} />;
}

function hasPanelUsage(info: ContextPanelInfo | null): boolean {
  return Boolean(
    (info?.requestCount ?? 0) > 0 ||
    (info?.promptTokens ?? 0) > 0 ||
    (info?.completionTokens ?? 0) > 0 ||
    (info?.totalTokens ?? 0) > 0 ||
    (info?.reasoningTokens ?? 0) > 0 ||
    (info?.cacheHitTokens ?? 0) > 0 ||
    (info?.cacheMissTokens ?? 0) > 0,
  );
}

function fmtDuration(ms: number, t: Translator): string {
  if (ms <= 0) return "-";
  const totalSeconds = Math.max(1, Math.round(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) return t("context.durationSeconds", { seconds });
  return t("context.durationMinutesSeconds", { minutes, seconds });
}

function sourceTokenTotal(row: Pick<ContextSourceRow, "promptTokens" | "completionTokens" | "totalTokens">): number {
  return row.totalTokens > 0 ? row.totalTokens : row.promptTokens + row.completionTokens;
}

function sourceTone(source: string): string {
  switch (source) {
    case "executor": return "teal";
    case "planner": return "blue";
    case "subagent": return "amber";
    case "compaction": return "slate";
    case "classifier": return "violet";
    case "title": return "rose";
    default: return "default";
  }
}

function sourceLabel(source: string, t: Translator): string {
  switch (source) {
    case "executor": return t("context.sourceExecutor");
    case "planner": return t("context.sourcePlanner");
    case "subagent": return t("context.sourceSubagent");
    case "compaction": return t("context.sourceCompaction");
    case "classifier": return t("context.sourceClassifier");
    case "title": return t("context.sourceTitle");
    default: return source;
  }
}

function providerValue(value?: string): string {
  const normalized = value?.trim();
  return normalized ? normalized : "-";
}

function profileBudgetLabel(profile: WireProfile | undefined, t: Translator): string {
  const used = typeof profile?.budgetUsed === "number" ? profile.budgetUsed : undefined;
  const limit = typeof profile?.budgetLimit === "number" ? profile.budgetLimit : undefined;
  const remaining = typeof profile?.budgetRemaining === "number" ? profile.budgetRemaining : undefined;
  if (used !== undefined && limit !== undefined && limit > 0) return `${fmtAnyNumber(used)} / ${fmtAnyNumber(limit)}`;
  if (remaining !== undefined) return t("dashboard.detail.budgetRemaining", { count: fmtAnyNumber(remaining) });
  return "-";
}

function formatBudgetLabel(value: number | undefined, t: Translator): string {
  const n = typeof value === "number" && Number.isFinite(value) ? Math.max(0, Math.round(value)) : 0;
  return n > 0 ? t("settings.frontierBudgetValue", { n: n.toLocaleString() }) : t("settings.frontierBudgetUnlimited");
}

function fmtBytes(value: number | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) return "-";
  return `${Math.round(value).toLocaleString()} bytes`;
}

function compressionPolicyLabel(settings: DashboardSettingsView | null | undefined, t: Translator): string {
  const policy = settings?.agent?.contextCompressionPolicy ?? "auto";
  switch (policy) {
    case "off": return t("settings.contextCompressionPolicy.off");
    case "aggressive": return t("settings.contextCompressionPolicy.aggressive");
    default: return t("settings.contextCompressionPolicy.auto");
  }
}

function modelDisplayLabel(modelLabel?: string, profile?: WireProfile, settings?: DashboardSettingsView | null): string {
  const direct = modelLabel?.trim();
  if (direct) return direct;
  const profileModel = profile?.model?.trim();
  if (profileModel) return profileModel;
  return providerValue(settings?.defaultModel);
}

function trimPreview(text: string | undefined, max = 160): string {
  const normalized = (text ?? "").replace(/\s+/g, " ").trim();
  if (!normalized) return "";
  return normalized.length > max ? `${normalized.slice(0, max - 1)}…` : normalized;
}

function turnMeta(turn: number | undefined, t: Translator): string {
  return typeof turn === "number" && turn > 0 ? t("dashboard.log.turnMeta", { n: turn }) : "";
}

function formatLogTime(time: number | undefined, locale: string, t: Translator): { label: string; iso?: string } {
  if (typeof time !== "number" || !Number.isFinite(time) || time <= 0) {
    return { label: t("dashboard.log.timeUnknown") };
  }
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) {
    return { label: t("dashboard.log.timeUnknown") };
  }
  return {
    label: new Intl.DateTimeFormat(locale, {
      year: "2-digit",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    }).format(date),
    iso: date.toISOString(),
  };
}

function toolCallNames(message: HistoryMessage): string[] {
  const names = asArray(message.toolCalls)
    .map((tool) => tool.name?.trim())
    .filter((name): name is string => Boolean(name));
  if (message.toolName?.trim()) names.push(message.toolName.trim());
  return Array.from(new Set(names));
}

function buildDashboardLogEntries({
  history,
  readFiles,
  changedFiles,
  locale,
  model,
  totalTokens,
  sessionCost,
  sessionCurrency,
  requestCount,
  sessionTurns,
  t,
}: {
  history: HistoryPage | null;
  readFiles: ReadFileRecord[];
  changedFiles: Array<{ path: string; latestPrompt?: string; latestTime?: number; turns?: number[] }>;
  locale: string;
  model: string;
  totalTokens: number;
  sessionCost?: number;
  sessionCurrency?: string;
  requestCount: number;
  sessionTurns?: number;
  t: Translator;
}): DashboardLogEntry[] {
  const entries: DashboardLogEntry[] = [];
  let sequence = 0;
  const modelChip = model.trim() && model !== "-" ? t("dashboard.log.model", { value: model }) : "";
  const tokenChip = totalTokens > 0 ? t("dashboard.log.tokens", { value: formatMetricTokens(totalTokens, locale).display }) : "";
  const requestChip = requestCount > 0 ? t("dashboard.log.requests", { count: requestCount }) : "";
  const turnsChip = typeof sessionTurns === "number" && sessionTurns > 0 ? t("dashboard.log.turns", { count: sessionTurns }) : "";
  const costLabel = typeof sessionCost === "number" && sessionCost > 0
    ? formatMoneyLocalized(sessionCost, sessionCurrency, { locale, empty: "dash" })
    : "";
  const costChip = costLabel && costLabel !== "-" ? t("dashboard.log.cost", { value: costLabel }) : "";
  const baseChips = [modelChip, tokenChip, requestChip, turnsChip, costChip].filter(Boolean);
  const pushEntry = (entry: Omit<DashboardLogEntry, "timeLabel" | "timeIso" | "chips" | "sequence"> & { chips?: string[] }) => {
    const timeInfo = formatLogTime(entry.time, locale, t);
    entries.push({
      ...entry,
      timeLabel: timeInfo.label,
      timeIso: timeInfo.iso,
      chips: [...baseChips, ...(entry.chips ?? [])],
      sequence: sequence++,
    });
  };
  const chipsForTurn = (turn: number | undefined, extra: string[] = []) => {
    const turnLabel = turnMeta(turn, t);
    return [...(turnLabel ? [turnLabel] : []), ...extra.filter(Boolean)];
  };
  const messages = asArray(history?.messages);
  messages.slice(-24).forEach((message, index) => {
    const turn = message.checkpointTurn;
    if (message.role === "user") {
      const body = trimPreview(message.submitText || message.content);
      if (body) {
        pushEntry({
          key: `user-${turn ?? index}-${index}`,
          tone: "user",
          title: t("dashboard.log.userRequest"),
          body,
          time: message.createdAt,
          chips: chipsForTurn(turn),
        });
      }
      return;
    }
    if (message.role === "assistant") {
      const body = trimPreview(message.content || message.reasoning);
      const tools = toolCallNames(message);
      if (tools.length > 0) {
        pushEntry({
          key: `tool-${turn ?? index}-${index}`,
          tone: "tool",
          title: t("dashboard.log.toolCalls"),
          body: tools.slice(0, 4).join(", "),
          time: message.createdAt,
          chips: chipsForTurn(turn, [t("dashboard.log.toolMeta", { n: tools.length })]),
        });
      }
      if (body) {
        pushEntry({
          key: `assistant-${turn ?? index}-${index}`,
          tone: "assistant",
          title: t("dashboard.log.assistant"),
          body,
          time: message.createdAt,
          chips: chipsForTurn(turn),
        });
      }
      return;
    }
    const systemBody = trimPreview(message.content || message.summary || message.archive || message.toolResultError);
    if (systemBody) {
      pushEntry({
        key: `system-${turn ?? index}-${index}`,
        tone: "system",
        title: message.role === "notice" ? t("dashboard.log.notice") : t("dashboard.log.sessionEvent"),
        body: systemBody,
        time: message.createdAt,
        chips: chipsForTurn(turn),
      });
    }
  });

  readFiles.slice(-5).forEach((file, index) => {
    pushEntry({
      key: `read-${file.path}-${file.turn}-${index}`,
      tone: "data",
      title: t("dashboard.log.readFile"),
      body: file.path,
      time: file.time,
      chips: chipsForTurn(file.turn),
    });
  });

  changedFiles.slice(-5).forEach((file, index) => {
    const turn = asArray(file.turns).slice(-1)[0];
    pushEntry({
      key: `changed-${file.path}-${turn ?? index}`,
      tone: "data",
      title: t("dashboard.log.changedFile"),
      body: file.latestPrompt ? `${file.path} · ${trimPreview(file.latestPrompt, 80)}` : file.path,
      time: file.latestTime,
      chips: chipsForTurn(turn),
    });
  });

  return entries
    .sort((a, b) => {
      const aTime = a.time && a.time > 0 ? a.time : Number.MIN_SAFE_INTEGER + a.sequence;
      const bTime = b.time && b.time > 0 ? b.time : Number.MIN_SAFE_INTEGER + b.sequence;
      if (aTime !== bTime) return aTime - bTime;
      return a.sequence - b.sequence;
    })
    .slice(-24)
    .reverse();
}

export function DashboardPanel({
  tabId,
  context,
  usage,
  providerStatus,
  contextInfo,
  historyPage,
  settingsView,
  balance,
  modelLabel,
  sessionTokens,
  sessionCost,
  sessionCurrency,
  sessionTurns,
  turnTokens,
  turnCost,
  refreshKey,
  onOpenSettings,
}: DashboardPanelProps) {
  const { locale, t } = useI18n();
  const [settings, setSettings] = useState<DashboardSettingsView | null>(settingsView ?? null);
  const [liveContextInfo, setLiveContextInfo] = useState<ContextPanelInfo | null>(null);
  const [liveHistoryPage, setLiveHistoryPage] = useState<HistoryPage | null>(null);
  const refreshSeq = useRef(0);
  const historyRefreshSeq = useRef(0);
  const panelInfo = contextInfo !== undefined ? contextInfo : liveContextInfo;
  const effectiveHistoryPage = historyPage !== undefined ? historyPage : liveHistoryPage;
  const effectiveSettings = settingsView !== undefined ? settingsView : settings;
  const metrics = useMemo(
    () => buildDashboardMetrics({ context, usage, providerStatus, sessionTokens, sessionCost, sessionCurrency, sessionTurns, turnTokens, turnCost }),
    [context, usage, providerStatus, sessionTokens, sessionCost, sessionCurrency, sessionTurns, turnTokens, turnCost],
  );
  const telemetry = useMemo(() => buildTelemetryStatus(effectiveSettings ?? undefined), [effectiveSettings]);
  const featureCount = useMemo(() => flattenDashboardFeatures(DASHBOARD_FEATURE_GROUPS).length, []);
  const refreshContextInfo = useCallback(async () => {
    if (!tabId || contextInfo !== undefined) return;
    const seq = ++refreshSeq.current;
    try {
      const next = await app.ContextPanel(tabId);
      if (refreshSeq.current === seq) setLiveContextInfo(next);
    } catch (e) {
      console.warn("dashboard context panel load failed", e);
    }
  }, [contextInfo, tabId]);

  const refreshHistory = useCallback(async () => {
    if (!tabId || historyPage !== undefined) return;
    const seq = ++historyRefreshSeq.current;
    try {
      const next = await app.HistoryPageForTab(tabId, 0, 12);
      if (historyRefreshSeq.current === seq) setLiveHistoryPage(next);
    } catch (e) {
      console.warn("dashboard history load failed", e);
    }
  }, [historyPage, tabId]);

  useEffect(() => {
    if (settingsView !== undefined) {
      setSettings(settingsView);
      return;
    }
    let cancelled = false;
    void app.Settings()
      .then((view) => {
        if (!cancelled) {
          setSettings({
            telemetry: view.telemetry,
            metrics: view.metrics,
            defaultModel: view.defaultModel,
            upgradeEnabled: view.upgradeEnabled,
            frontierModel: view.frontierModel,
            upgradeThreshold: view.upgradeThreshold,
            frontierBudget: view.frontierBudget,
            agent: view.agent,
          });
        }
      })
      .catch((e) => {
        console.warn("dashboard settings load failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, [refreshKey, settingsView]);

  useEffect(() => {
    refreshSeq.current += 1;
    if (contextInfo !== undefined) {
      setLiveContextInfo(null);
      return;
    }
    setLiveContextInfo(null);
    void refreshContextInfo();
    const id = window.setInterval(() => void refreshContextInfo(), 2000);
    return () => window.clearInterval(id);
  }, [contextInfo, refreshContextInfo]);

  useEffect(() => {
    if (contextInfo !== undefined) return;
    void refreshContextInfo();
  }, [contextInfo, refreshContextInfo, refreshKey]);

  useEffect(() => {
    historyRefreshSeq.current += 1;
    if (historyPage !== undefined) {
      setLiveHistoryPage(null);
      return;
    }
    setLiveHistoryPage(null);
    void refreshHistory();
    const id = window.setInterval(() => void refreshHistory(), 2500);
    return () => window.clearInterval(id);
  }, [historyPage, refreshHistory]);

  useEffect(() => {
    if (historyPage !== undefined) return;
    void refreshHistory();
  }, [historyPage, refreshHistory, refreshKey]);

  const panelHasUsage = hasPanelUsage(panelInfo);
  const usedTokens = context.used > 0 ? context.used : panelInfo?.usedTokens ?? 0;
  const windowTokens = context.window > 0 ? context.window : panelInfo?.windowTokens ?? 0;
  const promptTokens = panelHasUsage ? panelInfo?.promptTokens ?? 0 : usage?.promptTokens ?? 0;
  const completionTokens = panelHasUsage ? panelInfo?.completionTokens ?? 0 : usage?.completionTokens ?? 0;
  const reasoningTokens = panelHasUsage ? panelInfo?.reasoningTokens ?? 0 : usage?.reasoningTokens ?? 0;
  const totalTokens = panelInfo?.totalTokens && panelInfo.totalTokens > 0
    ? panelInfo.totalTokens
    : sessionTokens && sessionTokens > 0
      ? sessionTokens
      : usage?.totalTokens && usage.totalTokens > 0
        ? usage.totalTokens
        : promptTokens + completionTokens;
  const currentCacheHit = panelHasUsage ? panelInfo?.cacheHitTokens ?? 0 : usage?.cacheHitTokens ?? 0;
  const currentCacheMiss = panelHasUsage ? panelInfo?.cacheMissTokens ?? 0 : usage?.cacheMissTokens ?? 0;
  const sessionCacheHit = panelInfo?.sessionCacheHitTokens ?? usage?.sessionCacheHitTokens ?? context.cacheHitTokens ?? 0;
  const sessionCacheMiss = panelInfo?.sessionCacheMissTokens ?? usage?.sessionCacheMissTokens ?? context.cacheMissTokens ?? 0;
  const sessionCompletion = panelInfo?.sessionCompletionTokens ?? usage?.completionTokens ?? 0;
  const compression = contextCompressionBreakdown(panelInfo ?? {});
  const compressionEventLabel = compression.events > 0 ? fmtAnyNumber(compression.events) : "0";
  const compressionThresholdLabel = fmtBytes(effectiveSettings?.agent?.contextCompressionThresholdBytes);
  const compressionMaxLabel = fmtBytes(effectiveSettings?.agent?.contextCompressionMaxBytes);
  const compactionThresholdLabel = context.compactRatio ? `${Math.round(context.compactRatio * 100)}%` : "-";
  const readFiles = asArray(panelInfo?.readFiles);
  const changedFiles = asArray(panelInfo?.changedFiles);
  const eventTimes = [
    ...readFiles.map((file) => file.time),
    ...changedFiles.map((file) => file.latestTime ?? 0),
  ].filter((time) => time > 0);
  const derivedElapsed = eventTimes.length > 1 ? Math.max(...eventTimes) - Math.min(...eventTimes) : 0;
  const elapsed = panelInfo?.elapsedMs && panelInfo.elapsedMs > 0 ? panelInfo.elapsedMs : derivedElapsed;
  const requestCount = panelInfo?.requestCount && panelInfo.requestCount > 0 ? panelInfo.requestCount : readFiles.length + changedFiles.length;
  const sourceUsageRows = contextSourceRows(panelInfo, sessionCurrency);
  const sourceTotalTokens = sourceUsageRows.reduce((sum, row) => sum + sourceTokenTotal(row), 0);
  const profile = usage?.profile;
  const status = providerStatus ?? usage?.providerStatus;
  const currentModelLabel = modelDisplayLabel(modelLabel, profile, effectiveSettings);
  const logSessionCost = panelInfo?.sessionCost && panelInfo.sessionCost > 0 ? panelInfo.sessionCost : sessionCost;
  const logSessionCurrency = panelInfo?.sessionCurrency || sessionCurrency || usage?.currency;
  const logEntries = buildDashboardLogEntries({
    history: effectiveHistoryPage,
    readFiles,
    changedFiles,
    locale,
    model: currentModelLabel,
    totalTokens,
    sessionCost: logSessionCost,
    sessionCurrency: logSessionCurrency,
    requestCount,
    sessionTurns,
    t,
  });
  const providerMetricLabel = metrics.providerHealth !== "-" ? metrics.providerHealth : currentModelLabel;
  const balanceLabel = balance?.available && balance.display ? balance.display : providerValue(status?.balanceDisplay ?? status?.balanceStatus);
  const frontierConfigured = Boolean(effectiveSettings?.upgradeEnabled && effectiveSettings?.frontierModel?.trim());
  const frontierStatus = frontierConfigured ? t("settings.frontierEnabled") : t("settings.frontierDisabled");
  const frontierModel = providerValue(effectiveSettings?.frontierModel);
  const frontierThreshold = typeof effectiveSettings?.upgradeThreshold === "number" ? t("settings.frontierThresholdValue", { n: effectiveSettings.upgradeThreshold }) : "-";
  const frontierConfiguredBudget = formatBudgetLabel(effectiveSettings?.frontierBudget, t);
  const frontierLiveBudget = profileBudgetLabel(profile, t);
  const tokenMetric = (tokens: number | undefined) => formatMetricTokens(tokens, locale);
  const promptMetric = tokenMetric(promptTokens);
  const completionMetric = tokenMetric(completionTokens);
  const reasoningMetric = tokenMetric(reasoningTokens);
  const totalMetric = tokenMetric(totalTokens);
  const currentHitMetric = tokenMetric(currentCacheHit);
  const currentMissMetric = tokenMetric(currentCacheMiss);
  const sessionHitMetric = tokenMetric(sessionCacheHit);
  const sessionMissMetric = tokenMetric(sessionCacheMiss);
  const sessionCompletionMetric = tokenMetric(sessionCompletion);

  return (
    <div className="dashboard-panel">
      <div className="dashboard-panel__body">
        <section className="dashboard-panel__hero">
          <div className="dashboard-panel__hero-title">
            <BarChart3 size={18} />
            <div>
              <h2>{t("dashboard.title")}</h2>
              <span>{t("dashboard.subtitle", { count: featureCount })}</span>
            </div>
          </div>
          {onOpenSettings && (
            <button className="dashboard-panel__settings" type="button" onClick={onOpenSettings}>
              <Settings size={14} />
              <span>{t("settings.title")}</span>
            </button>
          )}
        </section>

        <section className="dashboard-panel__section">
          <SectionHeading icon={<Activity size={15} />} title={t("dashboard.monitoring")} meta={t("dashboard.monitoringMeta")} />
          <div className="dashboard-panel__metrics">
            <Metric icon={<Signal size={14} />} label={t("dashboard.metric.context")} value={fmtPct(metrics.contextPct)} />
            <Metric icon={<Zap size={14} />} label={t("dashboard.metric.cache")} value={fmtPct(metrics.cachePct)} />
            <Metric icon={<ListChecks size={14} />} label={t("dashboard.metric.turns")} value={fmtTurns(metrics.sessionTurns, t)} />
            <Metric icon={<Activity size={14} />} label={t("dashboard.metric.sessionTokens")} value={fmtNumber(metrics.sessionTokens)} />
            <Metric icon={<BarChart3 size={14} />} label={t("dashboard.metric.turnTokens")} value={fmtNumber(metrics.turnTokens)} />
            <Metric icon={<CircleDollarSign size={14} />} label={t("dashboard.metric.sessionCost")} value={formatMoneyLocalized(metrics.sessionCost.amount, metrics.sessionCost.currency, { locale, empty: "dash" })} />
            <Metric icon={<CircleDollarSign size={14} />} label={t("dashboard.metric.turnCost")} value={formatMoneyLocalized(metrics.turnCost.amount, metrics.turnCost.currency, { locale, empty: "dash" })} />
            <Metric icon={<Server size={14} />} label={t("dashboard.metric.provider")} value={providerMetricLabel} tone={providerMetricLabel === "-" ? undefined : "good"} />
          </div>
        </section>

        <section className="dashboard-panel__section">
          <SectionHeading icon={<BarChart3 size={15} />} title={t("dashboard.detailTitle")} meta={t("dashboard.detailMeta")} />
          <div className="dashboard-panel__detail-grid">
            <DetailCard icon={<Signal size={14} />} title={t("dashboard.detail.tokens")}>
              <DetailRow label={t("context.prompt")} value={promptMetric.display} title={promptMetric.exact} />
              <DetailRow label={t("context.completion")} value={completionMetric.display} title={completionMetric.exact} />
              <DetailRow label={t("context.reasoning")} value={reasoningMetric.display} title={reasoningMetric.exact} />
              <DetailRow label={t("context.total")} value={totalMetric.display} title={totalMetric.exact} />
              <DetailRow label={t("dashboard.detail.contextWindow")} value={`${fmtAnyNumber(usedTokens)} / ${fmtAnyNumber(windowTokens)}`} />
            </DetailCard>
            <DetailCard icon={<Zap size={14} />} title={t("dashboard.detail.cache")}>
              <DetailRow label={t("dashboard.detail.currentHit")} value={currentHitMetric.display} title={currentHitMetric.exact} />
              <DetailRow label={t("dashboard.detail.currentMiss")} value={currentMissMetric.display} title={currentMissMetric.exact} />
              <DetailRow label={t("dashboard.detail.currentRate")} value={formatCacheHitRate(currentCacheHit, currentCacheMiss)} />
              <DetailRow label={t("dashboard.detail.sessionHit")} value={sessionHitMetric.display} title={sessionHitMetric.exact} />
              <DetailRow label={t("dashboard.detail.sessionMiss")} value={sessionMissMetric.display} title={sessionMissMetric.exact} />
              <DetailRow label={t("dashboard.detail.sessionRate")} value={formatCacheHitRate(sessionCacheHit, sessionCacheMiss)} />
            </DetailCard>
            <DetailCard icon={<Server size={14} />} title={t("dashboard.detail.provider")}>
              <DetailRow label={t("dashboard.detail.role")} value={providerValue(status?.role ?? profile?.role)} />
              <DetailRow label={t("dashboard.detail.model")} value={currentModelLabel} />
              <DetailRow label={t("dashboard.detail.effort")} value={providerValue(profile?.effort)} />
              <DetailRow label={t("status.providerHealthLabel")} value={providerValue(status?.health)} />
              <DetailRow label={t("dashboard.detail.auth")} value={providerValue(status?.authStatus)} />
              <DetailRow label={t("status.rateLimitLabel")} value={providerValue(status?.rateLimit)} />
              <DetailRow label={t("status.balanceLabel")} value={balanceLabel} />
              <DetailRow label={t("dashboard.detail.frontierStatus")} value={frontierStatus} />
              <DetailRow label={t("dashboard.detail.frontierModel")} value={frontierModel} />
              <DetailRow label={t("dashboard.detail.frontierThreshold")} value={frontierThreshold} />
              <DetailRow label={t("status.frontierBudgetLabel")} value={frontierConfiguredBudget} />
              <DetailRow label={t("dashboard.detail.frontierLiveBudget")} value={frontierLiveBudget} />
            </DetailCard>
            <DetailCard icon={<ListChecks size={14} />} title={t("dashboard.detail.runtime")}>
              <DetailRow label={t("context.requests")} value={requestCount > 0 ? fmtAnyNumber(requestCount) : "-"} />
              <DetailRow label={t("context.time")} value={fmtDuration(elapsed, t)} />
              <DetailRow label={t("dashboard.detail.readFiles")} value={fmtAnyNumber(readFiles.length)} />
              <DetailRow label={t("dashboard.detail.changedFiles")} value={fmtAnyNumber(changedFiles.length)} />
              <DetailRow label={t("context.outputTokens")} value={sessionCompletionMetric.display} title={sessionCompletionMetric.exact} />
            </DetailCard>
            <DetailCard icon={<Activity size={14} />} title={t("dashboard.detail.compression")}>
              <DetailRow label={t("dashboard.detail.compressionEvents")} value={compressionEventLabel} />
              <DetailRow label={t("settings.contextCompressionPolicy")} value={compressionPolicyLabel(effectiveSettings, t)} />
              <DetailRow label={t("settings.contextCompressionThresholdBytes")} value={compressionThresholdLabel} />
              <DetailRow label={t("settings.contextCompressionMaxBytes")} value={compressionMaxLabel} />
              <DetailRow label={t("status.compactLabel")} value={compactionThresholdLabel} />
              <DetailRow label={t("dashboard.detail.rawTokens")} value={compression.rawTokensLabel === "-" ? "0" : compression.rawTokensLabel} />
              <DetailRow label={t("dashboard.detail.compressedTokens")} value={compression.compressedTokensLabel === "-" ? "0" : compression.compressedTokensLabel} />
              <DetailRow label={t("dashboard.detail.savedTokens")} value={compression.savedTokensLabel === "-" ? "0" : compression.savedTokensLabel} />
              <DetailRow label={t("dashboard.detail.rawChars")} value={fmtZeroNumber(compression.rawChars)} />
              <DetailRow label={t("dashboard.detail.savedChars")} value={fmtZeroNumber(compression.savedChars)} />
            </DetailCard>
          </div>
        </section>

        <section className="dashboard-panel__section">
          <SectionHeading
            icon={<BarChart3 size={15} />}
            title={t("context.sourceBreakdown")}
            meta={sourceUsageRows.length > 0 ? t("dashboard.detail.sourceMeta", { count: sourceUsageRows.length }) : t("dashboard.detail.noSources")}
          />
          {sourceUsageRows.length > 0 ? (
            <div className="dashboard-panel__source-list">
              {sourceUsageRows.map((row) => {
                const rowTotal = sourceTokenTotal(row);
                const sharePct = sourceTotalTokens > 0 ? (rowTotal / sourceTotalTokens) * 100 : 0;
                const inputMetric = tokenMetric(row.promptTokens);
                const outputMetric = tokenMetric(row.completionTokens);
                const hitMetric = tokenMetric(row.cacheHitTokens);
                const missMetric = tokenMetric(row.cacheMissTokens);
                const total = tokenMetric(rowTotal);
                const cacheRate = row.cacheHitTokens + row.cacheMissTokens > 0 ? formatCacheHitRate(row.cacheHitTokens, row.cacheMissTokens) : t("context.cacheNotReported");
                return (
                  <div className="dashboard-panel__source-row" key={row.source}>
                    <div className="dashboard-panel__source-head">
                      <span>
                        <i className={`dashboard-panel__source-dot dashboard-panel__source-tone--${sourceTone(row.source)}`} aria-hidden="true" />
                        {sourceLabel(row.source, t)}
                      </span>
                      <em>{sharePct > 0 ? `${sharePct.toFixed(0)}%` : "-"} · {t("context.sourceRequests", { count: row.requests })}</em>
                    </div>
                    <div className="dashboard-panel__source-stats">
                      <SourceStat label={t("context.total")} value={total.display} title={total.exact} />
                      <SourceStat label={t("context.sourceInput")} value={inputMetric.display} title={inputMetric.exact} />
                      <SourceStat label={t("context.sourceOutput")} value={outputMetric.display} title={outputMetric.exact} />
                      <SourceStat label={t("context.sourceCacheHit")} value={hitMetric.display} title={hitMetric.exact} />
                      <SourceStat label={t("context.sourceCacheMiss")} value={missMetric.display} title={missMetric.exact} />
                      <SourceStat label={t("context.sourceCacheRate")} value={cacheRate} />
                      <SourceStat label={t("context.sourceCost")} value={formatMoneyLocalized(row.cost, row.currency, { locale, empty: "dash" })} />
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="dashboard-panel__empty">{t("dashboard.detail.noSourceDetail")}</p>
          )}
        </section>

        <section className="dashboard-panel__section">
          <SectionHeading
            icon={<MessageSquare size={15} />}
            title={t("dashboard.logTitle")}
            meta={logEntries.length > 0 ? t("dashboard.logMeta", { count: logEntries.length }) : t("dashboard.logEmptyMeta")}
          />
          {logEntries.length > 0 ? (
            <div className="dashboard-panel__log-list">
              {logEntries.map((entry) => (
                <div className={`dashboard-panel__log-row dashboard-panel__log-row--${entry.tone}`} key={entry.key}>
                  <span className="dashboard-panel__log-icon" aria-hidden="true">
                    {entry.tone === "user" ? <UserRound size={13} /> : entry.tone === "tool" ? <Wrench size={13} /> : entry.tone === "data" ? <FileText size={13} /> : <MessageSquare size={13} />}
                  </span>
                  <div className="dashboard-panel__log-copy">
                    <div className="dashboard-panel__log-head">
                      <strong>{entry.title}</strong>
                      {entry.timeIso ? <time dateTime={entry.timeIso}>{entry.timeLabel}</time> : <em>{entry.timeLabel}</em>}
                    </div>
                    <p>{entry.body}</p>
                    {entry.chips.length > 0 && (
                      <div className="dashboard-panel__log-meta">
                        {entry.chips.map((chip, chipIndex) => (
                          <span key={`${entry.key}-chip-${chipIndex}`}>{chip}</span>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="dashboard-panel__empty">{t("dashboard.logEmpty")}</p>
          )}
        </section>

        <section className="dashboard-panel__section">
          <SectionHeading icon={<RadioTower size={15} />} title={t("dashboard.telemetryTitle")} meta={t("dashboard.telemetryMeta")} />
          <div className="dashboard-panel__signals">
            {telemetry.map((signal) => (
              <div className={`dashboard-panel__signal dashboard-panel__signal--${signal.enabled ? "on" : "off"}`} key={signal.id}>
                <div className="dashboard-panel__signal-head">
                  <span className="dashboard-panel__signal-icon">{signalIcon(signal)}</span>
                  <strong>{t(tk(signal.titleKey))}</strong>
                  <em>{enabledLabel(signal, t)}</em>
                </div>
                <p>{t(tk(signal.detailKey))}</p>
                <small><EyeOff size={12} />{t(tk(signal.privacyKey))}</small>
              </div>
            ))}
          </div>
        </section>

        <section className="dashboard-panel__section">
          <SectionHeading icon={<CheckCircle2 size={15} />} title={t("dashboard.featuresTitle")} meta={t("dashboard.featuresMeta", { count: featureCount })} />
          <div className="dashboard-panel__feature-groups">
            {DASHBOARD_FEATURE_GROUPS.map((group) => (
              <section className="dashboard-panel__feature-group" key={group.id}>
                <div className="dashboard-panel__feature-group-head">
                  <h3>{t(tk(group.titleKey))}</h3>
                  <span>{t(tk(group.summaryKey))}</span>
                </div>
                <ul>
                  {group.features.map((feature) => (
                    <li className={`dashboard-panel__feature dashboard-panel__feature--${feature.tone ?? "ready"}`} key={feature.id}>
                      <CheckCircle2 size={13} />
                      <span>
                        <strong>{t(tk(feature.titleKey))}</strong>
                        <small>{t(tk(feature.detailKey))}</small>
                      </span>
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}

function DetailCard({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <section className="dashboard-panel__detail-card">
      <header className="dashboard-panel__detail-card-head">
        {icon}
        <h4>{title}</h4>
      </header>
      <div className="dashboard-panel__detail-rows">{children}</div>
    </section>
  );
}

function DetailRow({ label, value, title }: { label: string; value: string; title?: string }) {
  const exactTitle = title && title !== value ? title : undefined;
  return (
    <div className="dashboard-panel__detail-row" aria-label={exactTitle ? `${label}: ${exactTitle}` : undefined}>
      <span>{label}</span>
      <strong title={exactTitle}>{value || "-"}</strong>
    </div>
  );
}

function SourceStat({ label, value, title }: { label: string; value: string; title?: string }) {
  const exactTitle = title && title !== value ? title : undefined;
  return (
    <div className="dashboard-panel__source-stat" aria-label={exactTitle ? `${label}: ${exactTitle}` : undefined}>
      <span>{label}</span>
      <strong title={exactTitle}>{value || "-"}</strong>
    </div>
  );
}

function SectionHeading({ icon, title, meta }: { icon: ReactNode; title: string; meta: string }) {
  return (
    <header className="dashboard-panel__section-head">
      <h3>{icon}{title}</h3>
      <span>{meta}</span>
    </header>
  );
}

function Metric({ icon, label, value, tone }: { icon: ReactNode; label: string; value: string; tone?: "good" | "warn" }) {
  const toneClass = tone ? ` dashboard-panel__metric--${tone}` : "";
  return (
    <div className={`dashboard-panel__metric${toneClass}`}>
      <span>{icon}{label}</span>
      <strong>{value || "-"}</strong>
    </div>
  );
}
