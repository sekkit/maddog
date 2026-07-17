import { useCallback, useRef, useState } from "react";
import { GitBranch, LoaderCircle, Plus, RefreshCw } from "lucide-react";
import { app } from "../lib/bridge";
import type { DeliveryWorktree, DeliveryWorktreeInspection } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";
import { Tooltip } from "./Tooltip";

export type DeliveryAction = "open" | "inspect" | "apply" | "discard";

export function deliveryActionsForState(state: DeliveryWorktree["state"]): DeliveryAction[] {
  if (state === "open") return ["open", "inspect", "apply", "discard"];
  if (state === "applied") return ["open", "inspect", "discard"];
  return [];
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function DeliveryWorktreeMenu() {
  const anchorRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [branch, setBranch] = useState("");
  const [deliveries, setDeliveries] = useState<DeliveryWorktree[]>([]);
  const [inspection, setInspection] = useState<DeliveryWorktreeInspection | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    setError("");
    try {
      setDeliveries(await app.ListDeliveryWorktrees());
    } catch (cause) {
      setError(messageOf(cause));
    }
  }, []);

  const show = () => {
    setOpen(true);
    void refresh();
  };

  const create = async () => {
    setBusy("create");
    setError("");
    try {
      await app.CreateDeliveryWorktree(branch);
      setBranch("");
      await refresh();
    } catch (cause) {
      setError(messageOf(cause));
    } finally {
      setBusy(null);
    }
  };

  const act = async (delivery: DeliveryWorktree, action: DeliveryAction) => {
    if ((action === "apply" || action === "discard") && typeof window !== "undefined") {
      const verb = action === "apply" ? "apply this delivery to the current branch" : "discard this delivery worktree";
      if (!window.confirm(`Explicitly ${verb}? Maddog will not push or delete the branch.`)) return;
    }
    setBusy(`${action}:${delivery.id}`);
    setError("");
    try {
      if (action === "open") await app.OpenDeliveryWorktree(delivery.id);
      if (action === "inspect") setInspection(await app.InspectDeliveryWorktree(delivery.id));
      if (action === "apply") await app.ApplyDeliveryWorktree(delivery.id);
      if (action === "discard") await app.DiscardDeliveryWorktree(delivery.id);
      if (action === "apply" || action === "discard") await refresh();
    } catch (cause) {
      setError(messageOf(cause));
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <Tooltip label="Delivery worktrees">
        <button
          ref={anchorRef}
          className={`workspace-iconbtn${open ? " workspace-iconbtn--on" : ""}`}
          type="button"
          aria-label="Delivery worktrees"
          aria-expanded={open}
          onClick={open ? () => setOpen(false) : show}
        >
          <GitBranch size={14} />
        </button>
      </Tooltip>
      <AnchoredPopover open={open} anchorRef={anchorRef} onClose={() => setOpen(false)} className="delivery-worktrees" align="end" offset={6} placement="bottom">
        <div className="delivery-worktrees__head">
          <div>
            <strong>Delivery worktrees</strong>
            <span>Explicit only — never auto-merged, pushed, or deleted.</span>
          </div>
          <button className="workspace-iconbtn" type="button" aria-label="Refresh delivery worktrees" onClick={() => void refresh()}>
            <RefreshCw size={13} />
          </button>
        </div>
        <div className="delivery-worktrees__create">
          <input value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="Optional branch name" aria-label="Delivery branch name" />
          <button type="button" onClick={() => void create()} disabled={busy !== null}>
            {busy === "create" ? <LoaderCircle size={13} className="spin" /> : <Plus size={13} />} Create
          </button>
        </div>
        {error && <div className="delivery-worktrees__error" role="alert">{error}</div>}
        <div className="delivery-worktrees__list">
          {deliveries.length === 0 ? <div className="delivery-worktrees__empty">No delivery worktrees.</div> : deliveries.map((delivery) => (
            <article key={delivery.id} className="delivery-worktrees__item">
              <div className="delivery-worktrees__meta">
                <strong>{delivery.branch}</strong>
                <span>{delivery.state} · {delivery.id}</span>
              </div>
              <div className="delivery-worktrees__actions">
                {deliveryActionsForState(delivery.state).map((action) => (
                  <button key={action} type="button" disabled={busy !== null} onClick={() => void act(delivery, action)}>
                    {busy === `${action}:${delivery.id}` ? "Working…" : action[0].toUpperCase() + action.slice(1)}
                  </button>
                ))}
              </div>
              {inspection?.delivery.id === delivery.id && (
                <div className="delivery-worktrees__inspection">
                  <span>{inspection.dirty ? "Uncommitted changes" : "Clean"} · {inspection.head.slice(0, 12)}</span>
                  {inspection.files.length > 0 && <ul>{inspection.files.map((file) => <li key={file}>{file}</li>)}</ul>}
                </div>
              )}
            </article>
          ))}
        </div>
      </AnchoredPopover>
    </>
  );
}
