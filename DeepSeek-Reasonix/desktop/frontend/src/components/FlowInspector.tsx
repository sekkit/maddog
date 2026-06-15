import { useMemo, useState } from "react";
import { Activity, Copy, Download, Trash2, X } from "lucide-react";
import { CodeViewer } from "./CodeViewer";
import { useT } from "../lib/i18n";
import { traceToJsonl, type FlowTraceEntry } from "../lib/flowTrace";

interface FlowInspectorProps {
  entries: FlowTraceEntry[];
  onClose: () => void;
  onClear: () => void;
  onExport: () => void;
}

function fmtTime(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function eventTone(kind: string): string {
  if (kind === "turn_done") return "done";
  if (kind === "tool_result") return "tool";
  if (kind === "tool_dispatch" || kind === "tool_progress") return "tool";
  if (kind === "usage") return "usage";
  if (kind === "advisor" || kind === "upgrade" || kind === "budget_exceeded") return "route";
  if (kind === "notice") return "notice";
  return "default";
}

export function FlowInspector({ entries, onClose, onClear, onExport }: FlowInspectorProps) {
  const t = useT();
  const [selectedSeq, setSelectedSeq] = useState<number | null>(null);
  const selected = entries.find((entry) => entry.seq === selectedSeq) ?? entries[entries.length - 1];
  const jsonl = useMemo(() => traceToJsonl(entries), [entries]);
  const selectedJson = selected ? JSON.stringify(selected, null, 2) : "";
  const counts = useMemo(() => {
    let tools = 0;
    let failures = 0;
    let usage = 0;
    for (const entry of entries) {
      if (entry.kind === "tool_dispatch") tools += 1;
      if (entry.kind === "tool_result" && entry.event.tool?.err) failures += 1;
      if (entry.kind === "usage") usage += 1;
    }
    return { tools, failures, usage };
  }, [entries]);

  return (
    <div className="drawer-backdrop drawer-backdrop--subtle" onClick={onClose} role="presentation">
      <aside className="drawer drawer--wide flow-inspector" role="dialog" aria-modal="true" aria-label={t("flow.title")} onClick={(e) => e.stopPropagation()}>
        <header className="drawer__head flow-inspector__head">
          <div>
            <div className="drawer__title flow-inspector__title"><Activity size={15} /> {t("flow.title")}</div>
            <div className="drawer__summary">
              {t("flow.summary", { events: entries.length, tools: counts.tools, usage: counts.usage, failures: counts.failures })}
            </div>
          </div>
          <div className="drawer__actions">
            <button className="flow-inspector__icon-btn" type="button" disabled={!entries.length} onClick={() => void navigator.clipboard?.writeText(jsonl)} aria-label={t("flow.copyTrace")}>
              <Copy size={14} />
            </button>
            <button className="flow-inspector__icon-btn" type="button" disabled={!entries.length} onClick={onExport} aria-label={t("flow.exportTrace")}>
              <Download size={14} />
            </button>
            <button className="flow-inspector__icon-btn" type="button" disabled={!entries.length} onClick={onClear} aria-label={t("flow.clearTrace")}>
              <Trash2 size={14} />
            </button>
            <button className="flow-inspector__icon-btn" type="button" onClick={onClose} aria-label={t("flow.close")}>
              <X size={14} />
            </button>
          </div>
        </header>
        <div className="drawer__body flow-inspector__body">
          {entries.length === 0 ? (
            <div className="flow-inspector__empty">{t("flow.empty")}</div>
          ) : (
            <>
              <section className="flow-inspector__timeline" aria-label={t("flow.timeline")}>
                {entries.map((entry) => (
                  <button
                    key={entry.seq}
                    type="button"
                    className={`flow-inspector__event${selected?.seq === entry.seq ? " flow-inspector__event--active" : ""}`}
                    data-tone={eventTone(entry.kind)}
                    onClick={() => setSelectedSeq(entry.seq)}
                  >
                    <span className="flow-inspector__seq">#{entry.seq}</span>
                    <span className="flow-inspector__kind">{entry.kind}</span>
                    <span className="flow-inspector__summary">{entry.summary}</span>
                    <span className="flow-inspector__time">{fmtTime(entry.ts)}</span>
                  </button>
                ))}
              </section>
              <section className="flow-inspector__detail" aria-label={t("flow.detail")}>
                <div className="flow-inspector__detail-head">
                  <span>{selected ? `#${selected.seq} ${selected.kind}` : t("flow.event")}</span>
                  <button type="button" onClick={() => void navigator.clipboard?.writeText(selectedJson)}>{t("flow.copyJson")}</button>
                </div>
                <CodeViewer value={selectedJson} language="json" maxHeight={520} />
              </section>
            </>
          )}
        </div>
      </aside>
    </div>
  );
}
