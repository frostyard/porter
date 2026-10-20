import { describe, expect, it, vi } from "vitest";
import type { Card, TurnList } from "../src/api";
import { App } from "../src/app";
import { emptyPending, pendingCount, pendingPage, pendingTurn, settleCard } from "../src/approvals";
import { dateTime } from "../src/time";
import { card, deferred, FakeTransport, flush, turn } from "./fake";

const flag = card("a1", {
  kind: "approval",
  status: "pending",
  app: "mail",
  tool: "flag_message",
  summary: "Flag Iris's message",
});
const capture = card("c1", { kind: "capture", status: "pending", tool: "append_daily", text: "Call Iris" });
const done = card("t1", { summary: "Wrote Porter/Briefs/2026-10-19.md" });

const routineTurn = turn(30, { role: "notice", at: "2026-10-19T06:30:00Z", text: "Brief ran.", cards: [flag, done] });
const replyTurn = turn(20, { at: "2026-10-18T20:00:00Z", cards: [capture, { ...capture, id: "c0", status: "done" }] });
const both = (): TurnList => ({ turns: [routineTurn, replyTurn], more: false });

async function setup(pending: () => TurnList | Promise<TurnList> = both) {
  const transport = new FakeTransport()
    .on("thread.subscribe", () => false)
    .on("thread.list", () => ({ turns: [], more: false }))
    .on("thread.pending", pending)
    .on("thread.confirm", (p) => turn(31, { role: "notice", text: `Done (${p.card}).` }))
    .on("thread.cancel", () => null)
    .on("activity.routines", () => [])
    .on("activity.list", () => ({ entries: [], more: false }));
  const root = document.createElement("div");
  document.body.replaceChildren(root);
  const app = new App({ root, transport, shell: { role: "app", open: vi.fn() } });
  const connected = app.connected();
  return { app, root, transport, connected };
}

const q = <E extends Element>(root: Element, sel: string) => root.querySelector<E>(sel) as E;
const view = (root: Element) => q<HTMLElement>(root, ".view.approvals");
const shown = (root: Element) => [...view(root).querySelectorAll<HTMLElement>(".card")].map((c) => c.dataset.card);
const badge = (root: Element) => q<HTMLElement>(root, '.tab[data-tab="approvals"] .tab-count');

function button(root: Element, id: string, label: string): HTMLButtonElement {
  const buttons = view(root).querySelectorAll<HTMLButtonElement>(`[data-card="${id}"] button`);
  const b = [...buttons].find((x) => x.textContent === label);
  if (b === undefined) throw new Error(`no ${label} button on ${id}`);
  return b;
}

describe("Approvals", () => {
  it("lists every waiting capture and approval, newest first, with its turn's time", async () => {
    const { root, connected } = await setup();
    await connected;
    q<HTMLButtonElement>(root, '.tab[data-tab="approvals"]').click();
    expect(shown(root)).toEqual(["a1", "c1"]);
    const items = [...view(root).querySelectorAll<HTMLElement>(".pending-item")];
    expect(items.map((i) => i.dataset.turn)).toEqual(["30", "20"]);
    expect(items[0]?.querySelector(".pending-time")?.textContent).toBe(dateTime("2026-10-19T06:30:00Z"));
    expect(view(root).querySelector('[data-card="a1"] .card-actions')).not.toBeNull();
    expect(view(root).querySelector('[data-card="c1"] textarea')).not.toBeNull();
    expect(badge(root).hidden).toBe(false);
    expect(badge(root).textContent).toBe("2");
  });

  it("acts on a card exactly as the thread does, and drops it once handled", async () => {
    const { root, transport, connected } = await setup();
    await connected;
    const answer = deferred<unknown>();
    transport.on("thread.confirm", () => answer.promise);
    button(root, "a1", "Approve").click();
    expect(transport.paramsOf("thread.confirm")).toStrictEqual([{ card: "a1" }]);
    expect(button(root, "a1", "Approve").disabled).toBe(true);
    expect(button(root, "a1", "Dismiss").disabled).toBe(true);
    answer.resolve(turn(31, { role: "notice", text: "Flagged." }));
    await flush();
    expect(shown(root)).toEqual(["c1"]);
    expect(badge(root).textContent).toBe("1");

    const area = q<HTMLTextAreaElement>(view(root), "textarea");
    area.value = "Call Iris on Friday";
    area.dispatchEvent(new Event("input", { bubbles: true }));
    button(root, "c1", "Cancel").click();
    await flush();
    expect(transport.paramsOf("thread.cancel")).toStrictEqual([{ card: "c1" }]);
    expect(shown(root)).toEqual([]);
    expect(badge(root).hidden).toBe(true);
    expect(q(view(root), ".view-note").textContent).toBe("Nothing is waiting for you.");
  });

  it("follows turn events: a card that stops waiting leaves, and a new one appears", async () => {
    const { app, root, connected } = await setup();
    await connected;
    app.event({ event: "turn", data: { ...routineTurn, cards: [{ ...flag, status: "done" }, done] } });
    expect(shown(root)).toEqual(["c1"]);
    expect(view(root).querySelector('[data-turn="30"]')).toBeNull();

    const another: Card = { ...flag, id: "a2", summary: "Archive the newsletter" };
    app.event({ event: "turn", data: turn(40, { role: "notice", cards: [another] }) });
    expect(shown(root)).toEqual(["a2", "c1"]);
    expect(badge(root).textContent).toBe("2");
    // A turn without waiting cards it never showed changes nothing.
    app.event({ event: "turn", data: turn(41, { cards: [done] }) });
    expect(shown(root)).toEqual(["a2", "c1"]);
  });

  it("keeps a capture's edit while its turn changes around it", async () => {
    const { app, root, connected } = await setup();
    await connected;
    const area = q<HTMLTextAreaElement>(view(root), '[data-card="c1"] textarea');
    area.value = "edited";
    area.dispatchEvent(new Event("input", { bubbles: true }));
    app.event({ event: "turn", data: { ...replyTurn, text: "More." } });
    expect(q<HTMLTextAreaElement>(view(root), '[data-card="c1"] textarea')).toBe(area);
    expect(area.value).toBe("edited");
  });

  it("applies turn events that arrive while thread.pending is in flight over its answer", async () => {
    const answer = deferred<TurnList>();
    const { app, root, connected } = await setup(() => answer.promise);
    await flush();
    app.event({ event: "turn", data: { ...routineTurn, cards: [{ ...flag, status: "cancelled" }, done] } });
    answer.resolve({ turns: [routineTurn, replyTurn], more: false });
    await connected;
    expect(shown(root)).toEqual(["c1"]);
  });

  it("says when it can't load", async () => {
    const { root, connected } = await setup(() => {
      throw new Error("boom");
    });
    await connected;
    expect(q(view(root), ".view-note.error").textContent).toBe("Couldn't load what's waiting: boom");
  });
});

describe("the Approvals reducer", () => {
  it("keeps only turns with a waiting card, newest first", () => {
    let s = pendingPage({ turns: [replyTurn, turn(5, { cards: [done] }), routineTurn], more: false });
    expect(s.turns.map((t) => t.id)).toEqual([30, 20]);
    expect(pendingCount(s)).toBe(2);
    expect(pendingTurn(s, turn(6, { cards: [done] }))).toBe(s);
    s = settleCard(s, "a1", { status: "done" });
    expect(s.turns.map((t) => t.id)).toEqual([20]);
    expect(settleCard(s, "nope", { status: "done" })).toBe(s);
    expect(pendingTurn(emptyPending, turn(9, { cards: [{ ...flag, kind: "tool" }] }))).toBe(emptyPending);
  });
});
