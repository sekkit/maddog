import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Activity, BarChart3, CheckCircle2, CircleDollarSign, EyeOff, ListChecks, RadioTower, Server, Settings, ShieldCheck, Signal, WifiOff, Zap } from "lucide-react";
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
import type { ContextInfo, SettingsView, WireProviderStatus, WireUsage } from "../lib/types";

interface DashboardPanelProps {
  context: ContextInfo;
  usage?: WireUsage;
  providerStatus?: WireProviderStatus;
  sessionTokens?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  sessionTurns?: number;
  turnTokens?: number;
  turnCost?: number;
  refreshKey?: number;
  onOpenSettings?: () => void;
}

function tk(key: string): DictKey {
  return key as DictKey;
}

function fmtNumber(value: number | undefined): string {
  if (typeof value !== "number" || value <= 0) return "-";
  return value.toLocaleString();
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

export function DashboardPanel({
  context,
  usage,
  providerStatus,
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
  const [settings, setSettings] = useState<Pick<SettingsView, "telemetry" | "metrics"> | null>(null);
  const metrics = useMemo(
    () => buildDashboardMetrics({ context, usage, providerStatus, sessionTokens, sessionCost, sessionCurrency, sessionTurns, turnTokens, turnCost }),
    [context, usage, providerStatus, sessionTokens, sessionCost, sessionCurrency, sessionTurns, turnTokens, turnCost],
  );
  const telemetry = useMemo(() => buildTelemetryStatus(settings ?? undefined), [settings]);
  const featureCount = useMemo(() => flattenDashboardFeatures(DASHBOARD_FEATURE_GROUPS).length, []);

  useEffect(() => {
    let cancelled = false;
    void app.Settings()
      .then((view) => {
        if (!cancelled) setSettings({ telemetry: view.telemetry, metrics: view.metrics });
      })
      .catch((e) => {
        console.warn("dashboard settings load failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, [refreshKey]);

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
            <Metric icon={<Server size={14} />} label={t("dashboard.metric.provider")} value={metrics.providerHealth} tone={metrics.providerHealth === "-" ? undefined : "good"} />
          </div>
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
