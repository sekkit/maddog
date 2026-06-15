import type { WireEvent } from "./types";

export interface FlowTraceEntry {
  seq: number;
  ts: number;
  kind: WireEvent["kind"];
  summary: string;
  event: WireEvent;
}

const SECRET_KEY_RE = /(api[_-]?key|authorization|token|secret|password|bearer|credential)/i;
const SECRET_VALUE_RE = /\b(sk-[A-Za-z0-9_-]{12,}|[A-Za-z0-9_-]{24,})\b/g;

export function traceSummary(e: WireEvent): string {
  switch (e.kind) {
    case "tool_dispatch":
      return e.tool?.name ? `tool ${e.tool.name} dispatched` : "tool dispatched";
    case "tool_progress":
      return e.tool?.name ? `tool ${e.tool.name} progress` : "tool progress";
    case "tool_result": {
      const name = e.tool?.name || "tool";
      return e.tool?.err ? `${name} failed` : `${name} completed`;
    }
    case "usage":
      return e.usage ? `${e.usage.totalTokens} tokens` : "usage";
    case "advisor":
      return e.advisor?.reason || e.text || "advisor";
    case "upgrade":
    case "budget_exceeded":
    case "skill_generated":
    case "skill_promoted":
    case "notice":
    case "phase":
      return e.text || e.kind;
    case "retrying":
      return `retry ${e.retryAttempt ?? 0}/${e.retryMax ?? 0}`;
    case "turn_done":
      return e.err ? `turn failed: ${e.err}` : "turn done";
    default:
      return e.kind;
  }
}

export function createTraceEntry(seq: number, e: WireEvent, now = Date.now()): FlowTraceEntry {
  return { seq, ts: now, kind: e.kind, summary: traceSummary(e), event: redactWireEvent(e) };
}

export function traceToJsonl(entries: FlowTraceEntry[]): string {
  return entries.map((entry) => JSON.stringify(entry)).join("\n") + (entries.length ? "\n" : "");
}

export function redactWireEvent<T>(value: T): T {
  return redactValue(value) as T;
}

function redactValue(value: unknown, key = ""): unknown {
  if (typeof value === "string") {
    if (SECRET_KEY_RE.test(key)) return "[redacted]";
    return value.replace(SECRET_VALUE_RE, "[redacted]");
  }
  if (Array.isArray(value)) return value.map((item) => redactValue(item, key));
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [childKey, childValue] of Object.entries(value)) {
      out[childKey] = SECRET_KEY_RE.test(childKey) ? "[redacted]" : redactValue(childValue, childKey);
    }
    return out;
  }
  return value;
}
