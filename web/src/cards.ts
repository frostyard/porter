// A Porter turn's cards. A tool card is one compact line: the app, the
// summary, and how it went, opening what it touched on a click. A capture
// card waits for the owner: its text in a textarea, with Confirm and
// Cancel, and then its outcome.

import { Code, errorCode, type Card } from "./api";
import { el } from "./dom";
import { classifyHref } from "./markdown";

/** What a card's controls call. */
export interface CardHost {
  /** Opens a lodge address (lodge.open). */
  open(url: string): void;
  /** Confirms a capture; text is the owner's edit, absent when unchanged. */
  confirm(card: Card, text?: string): Promise<void>;
  cancel(card: Card): Promise<void>;
}

/** confirmParams are thread.confirm's params: the edited text only when it changed. */
export function confirmParams(card: Card, edited: string): { card: string; text?: string } {
  return edited === (card.text ?? "") ? { card: card.id } : { card: card.id, text: edited };
}

/** sameCard reports whether two copies of a card would render alike. */
export function sameCard(a: Card, b: Card): boolean {
  return (
    a.id === b.id &&
    a.kind === b.kind &&
    a.status === b.status &&
    a.app === b.app &&
    a.tool === b.tool &&
    a.summary === b.summary &&
    a.url === b.url &&
    a.text === b.text &&
    a.detail === b.detail
  );
}

/**
 * renderCard renders a card. `previous` is the element it replaces, whose
 * capture edit carries over while the card is still pending.
 */
export function renderCard(card: Card, host: CardHost, previous?: HTMLElement): HTMLElement {
  if (card.kind === "capture") return capture(card, host, previous);
  return tool(card, host);
}

function appLabel(card: Card): HTMLElement {
  const app = el("span", "card-app", card.app || "porter");
  if (card.tool) app.title = `${card.app}: ${card.tool}`;
  return app;
}

function tool(card: Card, host: CardHost): HTMLElement {
  const root = el("div", `card tool ${card.status}`);
  root.dataset.card = card.id;
  const target = card.url ? classifyHref(card.url) : ({ kind: "text" } as const);
  let line: HTMLElement;
  if (card.url && target.kind !== "text") {
    const url = card.url;
    const a = el("a", "card-line");
    a.setAttribute("href", target.kind === "lodge" ? target.path : target.url.href);
    a.title = url;
    a.addEventListener("click", (e) => {
      e.preventDefault();
      host.open(url);
    });
    line = a;
  } else {
    line = el("div", "card-line");
  }
  line.append(appLabel(card), el("span", "card-summary", card.summary || card.tool));
  line.append(statusLabel(card));
  root.append(line);
  if (card.status === "failed" && card.detail) root.append(el("div", "card-detail", card.detail));
  return root;
}

function statusLabel(card: Card): HTMLElement {
  switch (card.status) {
    case "done": {
      const s = el("span", "card-status ok", "✓");
      s.title = "Done";
      s.setAttribute("aria-label", "done");
      return s;
    }
    case "failed":
      return el("span", "card-status failed", "Failed");
    case "cancelled":
      return el("span", "card-status", "Cancelled");
    case "pending":
      return el("span", "card-status", "…");
  }
  return el("span", "card-status", String(card.status));
}

function errorText(err: unknown): string {
  switch (errorCode(err)) {
    case Code.conflict:
      return "This was already handled.";
    case Code.notFound:
      return "This capture no longer exists.";
    case Code.unavailable:
      return "Notes isn't reachable right now. Try again in a moment.";
    case Code.disconnected:
      return "Not connected. Try again once Porter reconnects.";
  }
  return err instanceof Error && err.message ? `That didn't work: ${err.message}` : "That didn't work.";
}

function capture(card: Card, host: CardHost, previous?: HTMLElement): HTMLElement {
  const root = el("div", `card capture ${card.status}`);
  root.dataset.card = card.id;
  const head = el("div", "card-line");
  head.append(appLabel(card), el("span", "card-summary", card.summary || "Add to today's daily note"));
  root.append(head);

  if (card.status !== "pending") {
    if (card.text && card.status !== "failed") root.append(el("blockquote", "capture-text", card.text));
    root.append(outcome(card));
    return root;
  }

  const area = el("textarea", "capture-edit");
  area.setAttribute("aria-label", "Text to add to today's daily note");
  area.rows = 3;
  area.value = card.text ?? "";
  // Keep the owner's edit across a re-render while the card is pending.
  const prev = previous?.querySelector<HTMLTextAreaElement>("textarea.capture-edit");
  if (prev && previous?.dataset.card === card.id && prev.dataset.edited === "true") {
    area.value = prev.value;
    area.dataset.edited = "true";
  }
  area.addEventListener("input", () => {
    area.dataset.edited = "true";
    error.hidden = true;
    confirm.disabled = area.value.trim() === "";
  });

  const actions = el("div", "card-actions");
  const confirm = el("button", "primary", "Confirm");
  confirm.type = "button";
  confirm.disabled = area.value.trim() === "";
  const cancel = el("button", undefined, "Cancel");
  cancel.type = "button";
  actions.append(confirm, cancel);
  const error = el("div", "card-error");
  error.setAttribute("role", "alert");
  error.hidden = true;

  const run = (action: () => Promise<void>) => {
    confirm.disabled = true;
    cancel.disabled = true;
    area.readOnly = true;
    error.hidden = true;
    action().catch((err: unknown) => {
      confirm.disabled = area.value.trim() === "";
      cancel.disabled = false;
      area.readOnly = false;
      error.textContent = errorText(err);
      error.hidden = false;
    });
  };
  confirm.addEventListener("click", () => {
    // An empty edit would read as "no edit" on the wire, and append the original.
    if (area.value.trim() === "") return;
    const p = confirmParams(card, area.value);
    run(() => host.confirm(card, p.text));
  });
  cancel.addEventListener("click", () => run(() => host.cancel(card)));

  root.append(area, actions, error);
  return root;
}

function outcome(card: Card): HTMLElement {
  switch (card.status) {
    case "done":
      return el("div", "card-outcome ok", "Added to today's daily note.");
    case "cancelled":
      return el("div", "card-outcome", "Cancelled.");
    case "failed":
      return el("div", "card-outcome failed", card.detail ? `Not added: ${card.detail}` : "Not added.");
  }
  return el("div", "card-outcome", "");
}
