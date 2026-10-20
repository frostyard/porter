// A Porter turn's cards. A tool card is one compact line: the app, the
// summary, and how it went, opening what it touched on a click. A capture
// card waits for the owner: its text in a textarea, with Confirm and
// Cancel, and then its outcome. An approval card is an action a routine
// proposed: what it would do and with what, with Approve and Dismiss, and
// then its outcome. Read-only cards (Activity) show where a pending card
// stands without its buttons.

import { Code, errorCode, type Card } from "./api";
import { el } from "./dom";
import { classifyHref } from "./markdown";

/** What a card's controls call. */
export interface CardHost {
  /** Opens a lodge address (lodge.open). */
  open(url: string): void;
  /** Confirms a capture, text being the owner's edit (absent when unchanged), or approves an approval. */
  confirm(card: Card, text?: string): Promise<void>;
  /** Cancels a capture, or dismisses an approval. */
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
    a.detail === b.detail &&
    a.args === b.args
  );
}

/** How long an approval's argument value may be before it is cut. */
const ARG_MAX = 160;

/** One line of an approval's arguments; `full` is the value before it was cut. */
export interface ArgLine {
  key: string;
  value: string;
  full: string;
}

/**
 * argLines reads an approval's args, a JSON object, as key and value
 * lines, each value flattened to one line and cut at max characters.
 * Anything but a JSON object reads as no lines.
 */
export function argLines(args: string | undefined, max = ARG_MAX): ArgLine[] {
  if (!args) return [];
  let v: unknown;
  try {
    v = JSON.parse(args);
  } catch {
    return [];
  }
  if (typeof v !== "object" || v === null || Array.isArray(v)) return [];
  return Object.entries(v).map(([key, x]) => {
    const full = (typeof x === "string" ? x : JSON.stringify(x)).replace(/\s+/g, " ").trim();
    return { key, value: full.length > max ? `${full.slice(0, max - 1)}…` : full, full };
  });
}

/**
 * renderCard renders a card. `previous` is the element it replaces, whose
 * capture edit carries over while the card is still pending. A read-only
 * card has no buttons.
 */
export function renderCard(card: Card, host: CardHost, previous?: HTMLElement, readOnly = false): HTMLElement {
  if (card.kind === "capture") return capture(card, host, previous, readOnly);
  if (card.kind === "approval") return approval(card, host, readOnly);
  return tool(card, host);
}

/**
 * lodgeLink is a link to a lodge address or an https URL that opens with
 * open (lodge.open), or null for anything else.
 */
export function lodgeLink(
  url: string,
  open: (url: string) => void,
  className: string,
  text?: string,
): HTMLAnchorElement | null {
  const target = classifyHref(url);
  if (target.kind === "text") return null;
  const a = el("a", className, text);
  a.setAttribute("href", target.kind === "lodge" ? target.path : target.url.href);
  a.title = url;
  a.addEventListener("click", (e) => {
    e.preventDefault();
    open(url);
  });
  return a;
}

function appLabel(card: Card): HTMLElement {
  const app = el("span", "card-app", card.app || "porter");
  if (card.tool) app.title = `${card.app}: ${card.tool}`;
  return app;
}

function tool(card: Card, host: CardHost): HTMLElement {
  const root = el("div", `card tool ${card.status}`);
  root.dataset.card = card.id;
  const line = (card.url ? lodgeLink(card.url, (url) => host.open(url), "card-line") : null) ?? el("div", "card-line");
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

function errorText(err: unknown, card: Card): string {
  const approval = card.kind === "approval";
  switch (errorCode(err)) {
    case Code.conflict:
      return "This was already handled.";
    case Code.notFound:
      return approval ? "This approval no longer exists." : "This capture no longer exists.";
    case Code.unavailable:
      return `${approval ? appName(card.app) : "Notes"} isn't reachable right now. Try again in a moment.`;
    case Code.disconnected:
      return "Not connected. Try again once Porter reconnects.";
  }
  return err instanceof Error && err.message ? `That didn't work: ${err.message}` : "That didn't work.";
}

function appName(app: string): string {
  return app ? `${app[0]?.toUpperCase()}${app.slice(1)}` : "That app";
}

function errorBox(): HTMLElement {
  const error = el("div", "card-error");
  error.setAttribute("role", "alert");
  error.hidden = true;
  return error;
}

function capture(card: Card, host: CardHost, previous: HTMLElement | undefined, readOnly: boolean): HTMLElement {
  const root = el("div", `card capture ${card.status}`);
  root.dataset.card = card.id;
  const head = el("div", "card-line");
  head.append(appLabel(card), el("span", "card-summary", card.summary || "Add to today's daily note"));
  root.append(head);

  if (card.status !== "pending" || readOnly) {
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
  const error = errorBox();

  const run = (action: () => Promise<void>) => {
    confirm.disabled = true;
    cancel.disabled = true;
    area.readOnly = true;
    error.hidden = true;
    action().catch((err: unknown) => {
      confirm.disabled = area.value.trim() === "";
      cancel.disabled = false;
      area.readOnly = false;
      error.textContent = errorText(err, card);
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
    case "pending":
      return el("div", "card-outcome", "Pending confirmation.");
  }
  return el("div", "card-outcome", "");
}

function approval(card: Card, host: CardHost, readOnly: boolean): HTMLElement {
  const root = el("div", `card approval ${card.status}`);
  root.dataset.card = card.id;
  const head = el("div", "card-line");
  head.append(appLabel(card), el("span", "card-summary", card.summary || card.tool));
  root.append(head);
  if (card.tool) root.append(el("div", "card-tool", card.tool));
  const lines = argLines(card.args);
  if (lines.length > 0) {
    const dl = el("dl", "card-args");
    for (const l of lines) {
      const dd = el("dd", undefined, l.value);
      if (l.value !== l.full) dd.title = l.full;
      dl.append(el("dt", undefined, l.key), dd);
    }
    root.append(dl);
  }

  if (card.status !== "pending") {
    root.append(approvalOutcome(card, host));
    return root;
  }
  if (readOnly) {
    root.append(el("div", "card-outcome", "Pending approval."));
    return root;
  }

  const actions = el("div", "card-actions");
  const approve = el("button", "primary", "Approve");
  approve.type = "button";
  const dismiss = el("button", undefined, "Dismiss");
  dismiss.type = "button";
  actions.append(approve, dismiss);
  const error = errorBox();
  const run = (action: () => Promise<void>) => {
    approve.disabled = true;
    dismiss.disabled = true;
    error.hidden = true;
    action().catch((err: unknown) => {
      approve.disabled = false;
      dismiss.disabled = false;
      error.textContent = errorText(err, card);
      error.hidden = false;
    });
  };
  approve.addEventListener("click", () => run(() => host.confirm(card)));
  dismiss.addEventListener("click", () => run(() => host.cancel(card)));
  root.append(actions, error);
  return root;
}

function approvalOutcome(card: Card, host: CardHost): HTMLElement {
  switch (card.status) {
    case "done": {
      const done = el("div", "card-outcome ok", "Approved.");
      const link = card.url ? lodgeLink(card.url, (url) => host.open(url), "card-link", "Open") : null;
      if (link !== null) done.append(" ", link);
      return done;
    }
    case "cancelled":
      return el("div", "card-outcome", "Dismissed.");
    case "failed":
      return el("div", "card-outcome failed", card.detail ? `Failed: ${card.detail}` : "Failed.");
  }
  return el("div", "card-outcome", "");
}
