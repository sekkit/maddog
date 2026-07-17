import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { DeliveryWorktreeMenu, deliveryActionsForState } from "../components/DeliveryWorktreeMenu";

function check(name: string, ok: boolean) {
  if (!ok) throw new Error(`FAIL ${name}`);
  console.log(`PASS ${name}`);
}

const html = renderToStaticMarkup(createElement(DeliveryWorktreeMenu));
check("delivery lifecycle is reachable from the workspace UI", html.includes('aria-label="Delivery worktrees"'));
check("open delivery exposes every explicit action", deliveryActionsForState("open").join(",") === "open,inspect,apply,discard");
check("applied delivery remains inspectable and explicitly discardable", deliveryActionsForState("applied").join(",") === "open,inspect,discard");
check("discard tombstone has no mutating action", deliveryActionsForState("discarded").length === 0);
