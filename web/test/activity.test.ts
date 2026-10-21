import { describe, expect, it, vi } from "vitest";
import type { Entry, EntryList, Routine } from "../src/api";
import { addEntry, emptyActivity, entryTurn, newestEntries, olderEntries } from "../src/activity";
import { App } from "../src/app";
import { dateTime, dayLabel } from "../src/time";
import { card, entry, FakeTransport, flush, routine, turn } from "./fake";

const brief = routine("Morning brief", {
  next: "2026-10-20T05:30:00Z",
  last: "2026-10-19T05:30:00Z",
  lastOk: true,
});
const review = routine("Weekly review", {
  url: undefined,
  enabled: false,
  schedule: "0 18 * * 0",
  last: "2026-10-18T17:00:00Z",
  lastOk: false,
});
const broken = routine("Broken", { valid: false, error: "schedule: expected 5 fields, got 4", schedule: "30 6 * *" });

const approval = card("a1", {
  kind: "approval",
  status: "pending",
  app: "mail",
  tool: "flag_message",
  summary: "Flag Iris's message",
});

async function setup(o: { routines?: Routine[]; list?: (p: Record<string, unknown>) => EntryList } = {}) {
  const transport = new FakeTransport()
    .on("thread.subscribe", () => false)
    .on("thread.list", () => ({ turns: [], more: false }))
    .on("thread.pending", () => ({ turns: [], more: false }))
    .on("activity.routines", () => o.routines ?? [brief, review, broken])
    .on(
      "activity.list",
      o.list ??
        (() => ({
          entries: [
            entry(3, { url: "/notes/1/Porter/Briefs/2026-10-19.md", turn: 40, cards: [approval] }),
            entry(2, { kind: "skipped", ok: false, text: "The last run was still going." }),
            entry(1, { kind: "run", ok: false, routine: "Weekly review", text: "mail is down" }),
          ],
          more: false,
        })),
    );
  const open = vi.fn<(url: string) => void>();
  const root = document.createElement("div");
  document.body.replaceChildren(root);
  const app = new App({ root, transport, shell: { role: "app", open } });
  await app.connected();
  return { app, root, transport, open };
}

const q = <E extends Element>(root: Element, sel: string) => root.querySelector<E>(sel) as E;
const tab = (root: Element, name: string) => q<HTMLButtonElement>(root, `.tab[data-tab="${name}"]`);

describe("the full app's tabs", () => {
  it("shows the thread first, and switches to Activity and Approvals", async () => {
    const { root } = await setup();
    expect([...root.querySelectorAll(".tabs .tab")].map((t) => t.textContent)).toEqual(["Thread", "Activity", "Approvals"]);
    expect(tab(root, "thread").getAttribute("aria-selected")).toBe("true");
    const thread = q<HTMLElement>(root, ".thread-wrap");
    const composer = q<HTMLElement>(root, ".composer");
    const activity = q<HTMLElement>(root, ".view.activity");
    const approvals = q<HTMLElement>(root, ".view.approvals");
    expect([thread.hidden, composer.hidden, activity.hidden, approvals.hidden]).toEqual([false, false, true, true]);

    tab(root, "activity").click();
    expect(tab(root, "activity").getAttribute("aria-selected")).toBe("true");
    expect(tab(root, "thread").getAttribute("aria-selected")).toBe("false");
    expect([thread.hidden, composer.hidden, activity.hidden, approvals.hidden]).toEqual([true, true, false, true]);
    expect(q<HTMLElement>(root, "input.search").hidden).toBe(true);

    tab(root, "approvals").click();
    expect([thread.hidden, composer.hidden, activity.hidden, approvals.hidden]).toEqual([true, true, true, false]);
    tab(root, "thread").click();
    expect([thread.hidden, composer.hidden, activity.hidden, approvals.hidden]).toEqual([false, false, true, true]);
    expect(q<HTMLElement>(root, "input.search").hidden).toBe(false);
  });

  it("moves between tabs with the arrow keys", async () => {
    const { root } = await setup();
    const key = (k: string) =>
      q(root, ".tabs").dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true }));
    key("ArrowRight");
    expect(tab(root, "activity").getAttribute("aria-selected")).toBe("true");
    key("End");
    expect(tab(root, "approvals").getAttribute("aria-selected")).toBe("true");
    key("ArrowRight");
    expect(tab(root, "thread").getAttribute("aria-selected")).toBe("true");
    key("ArrowLeft");
    expect(tab(root, "approvals").getAttribute("aria-selected")).toBe("true");
  });

  it("are not in the companion, which shows only the thread", async () => {
    const transport = new FakeTransport()
      .on("thread.subscribe", () => false)
      .on("thread.list", () => ({ turns: [], more: false }));
    const root = document.createElement("div");
    document.body.replaceChildren(root);
    const app = new App({ root, transport, shell: { role: "companion", open: vi.fn() } });
    await app.connected();
    app.event({ event: "activity", data: entry(9) });
    expect(root.querySelector(".tabs")).toBeNull();
    expect(root.querySelector(".view")).toBeNull();
    expect(transport.calls.map((c) => c.method)).toEqual(["thread.subscribe", "thread.list"]);
  });

  it("says in the bar when Porter is disconnected off the thread", async () => {
    const { app, root } = await setup();
    tab(root, "activity").click();
    app.disconnected();
    expect(q<HTMLElement>(root, ".bar-status").hidden).toBe(false);
    expect(q(root, ".bar-status").textContent).toBe("Reconnecting to Porter…");
    tab(root, "thread").click();
    expect(q<HTMLElement>(root, ".bar-status").hidden).toBe(true);
  });
});

describe("Activity", () => {
  it("lists the routines: schedule, zone, whether on, next and last runs, and an invalid one's error", async () => {
    const { root, open } = await setup();
    const rows = [...root.querySelectorAll<HTMLElement>(".routine")];
    expect(rows.map((r) => r.dataset.name)).toEqual(["Morning brief", "Weekly review", "Broken"]);
    const [b, w, x] = rows as [HTMLElement, HTMLElement, HTMLElement];

    const name = b.querySelector("a.routine-name") as HTMLAnchorElement;
    expect(name.textContent).toBe("Morning brief");
    name.click();
    expect(open).toHaveBeenCalledWith("/notes/1/Porter/Routines/Morning brief.md");
    expect(b.querySelector(".routine-schedule")?.textContent).toBe("30 6 * * 1-5");
    expect(b.querySelector(".routine-zone")?.textContent).toBe("Europe/Lisbon");
    expect(b.querySelector(".badge")?.textContent).toBe("On");
    expect(b.querySelector(".routine-next time")?.textContent).toBe(dateTime("2026-10-20T05:30:00Z"));
    expect(b.querySelector(".routine-last time")?.textContent).toBe(dateTime("2026-10-19T05:30:00Z"));
    expect(b.querySelector(".routine-last .run-ok")?.textContent).toBe("ok");

    expect(w.querySelector("a")).toBeNull();
    expect(w.querySelector(".routine-name")?.textContent).toBe("Weekly review");
    expect(w.classList.contains("off")).toBe(true);
    expect(w.querySelector(".badge")?.textContent).toBe("Off");
    expect(w.querySelector(".routine-next")).toBeNull();
    expect(w.querySelector(".routine-last .run-failed")?.textContent).toBe("failed");

    expect(x.classList.contains("invalid")).toBe(true);
    expect(x.querySelector(".badge")?.textContent).toBe("Invalid");
    expect(x.querySelector(".routine-error")?.textContent).toBe("schedule: expected 5 fields, got 4");
  });

  it("says when there are no routines, and when they fail to load", async () => {
    const { root, transport, app } = await setup({ routines: [] });
    expect(q(root, ".view.activity .view-note").textContent).toMatch(/No routines yet/);
    transport.on("activity.routines", () => {
      throw new Error("boom");
    });
    app.event({ event: "activity", data: entry(9) });
    await flush();
    expect(q(root, ".view.activity .view-note.error").textContent).toBe("Couldn't load the routines: boom");
  });

  it("lists the entries newest first: time, routine, kind, text, link and read-only cards", async () => {
    const { root, open } = await setup();
    const rows = [...root.querySelectorAll<HTMLElement>(".entry")];
    expect(rows.map((r) => r.dataset.id)).toEqual(["3", "2", "1"]);
    const [ran, skipped, failed] = rows as [HTMLElement, HTMLElement, HTMLElement];

    expect(ran.querySelector(".entry-kind")?.textContent).toBe("Ran");
    expect(ran.querySelector(".entry-routine")?.textContent).toBe("Morning brief");
    expect(ran.querySelector(".entry-time")?.textContent).toBe(dateTime("2026-10-19T06:30:00Z"));
    expect(ran.querySelector(".entry-text")?.textContent).toBe("Wrote the brief.");
    (ran.querySelector("a.entry-link") as HTMLAnchorElement).click();
    expect(open).toHaveBeenCalledWith("/notes/1/Porter/Briefs/2026-10-19.md");
    expect(ran.querySelector(".card.approval .card-summary")?.textContent).toBe("Flag Iris's message");
    expect(ran.querySelectorAll("button")).toHaveLength(0);
    expect(ran.querySelector(".card.approval .card-outcome")?.textContent).toBe("Pending approval.");

    expect(skipped.querySelector(".entry-kind")?.textContent).toBe("Skipped");
    expect(skipped.querySelector("a")).toBeNull();
    expect(failed.querySelector(".entry-kind")?.textContent).toBe("Failed");
    expect(failed.classList.contains("failed")).toBe(true);
  });

  it("adds an activity event's entry at the top, and reads the routines again", async () => {
    const { app, root, transport } = await setup();
    expect(transport.paramsOf("activity.routines")).toHaveLength(1);
    app.event({ event: "activity", data: entry(4, { routine: "Weekly review", text: "Wrote the review." }) });
    const first = q<HTMLElement>(root, ".entry");
    expect(first.dataset.id).toBe("4");
    expect(first.querySelector(".entry-text")?.textContent).toBe("Wrote the review.");
    expect(transport.paramsOf("activity.routines")).toHaveLength(2);
    // The same entry again replaces it rather than adding it twice.
    app.event({ event: "activity", data: entry(4, { text: "Wrote the review again." }) });
    expect(root.querySelectorAll('.entry[data-id="4"]')).toHaveLength(1);
    expect(app.state.turns).toEqual([]);
  });

  it("reads the routines again when the tab opens", async () => {
    const { root, transport } = await setup();
    tab(root, "activity").click();
    expect(transport.paramsOf("activity.routines")).toHaveLength(2);
  });

  it("loads older entries before the last one", async () => {
    const { root, transport } = await setup({
      list: (p) =>
        p.before === undefined
          ? { entries: [entry(60), entry(59)], more: true }
          : { entries: [entry(58), entry(57)], more: false },
    });
    const more = q<HTMLButtonElement>(root, ".view-more button");
    expect(more.textContent).toBe("Load older activity");
    more.click();
    expect(q<HTMLButtonElement>(root, ".view-more button").disabled).toBe(true);
    await flush();
    expect(transport.paramsOf("activity.list")).toStrictEqual([{ limit: 50 }, { before: 59, limit: 50 }]);
    expect([...root.querySelectorAll<HTMLElement>(".entry")].map((r) => r.dataset.id)).toEqual(["60", "59", "58", "57"]);
    expect(root.querySelector(".view-more button")).toBeNull();
  });

  it("brings an entry's cards up to date with its turn's events", async () => {
    const { app, root } = await setup();
    app.event({ event: "turn", data: turn(40, { role: "notice", cards: [{ ...approval, status: "done" }] }) });
    expect(q(root, '.entry[data-id="3"] .card.approval .card-outcome').textContent).toBe("Approved.");
  });
});

describe("the Activity reducer", () => {
  const ids = (s: { entries: readonly Entry[] }) => s.entries.map((e) => e.id);

  it("keeps entries newest first, replacing those with the same id", () => {
    let s = newestEntries(emptyActivity, { entries: [entry(5), entry(4)], more: true });
    s = addEntry(s, entry(6));
    s = addEntry(s, entry(5, { text: "again" }));
    s = olderEntries(s, { entries: [entry(3)], more: false });
    expect(ids(s)).toEqual([6, 5, 4, 3]);
    expect(s.entries[1]?.text).toBe("again");
    expect(s.more).toBe(false);
  });

  it("merges a reconnect's newest page that reaches the shown entries, and replaces one that leaves a gap", () => {
    let s = newestEntries(emptyActivity, { entries: [entry(5), entry(4)], more: true });
    s = olderEntries(s, { entries: [entry(3)], more: true });
    s = newestEntries(s, { entries: [entry(6), entry(5)], more: true });
    expect(ids(s)).toEqual([6, 5, 4, 3]);
    expect(s.more).toBe(true);
    s = newestEntries(s, { entries: [entry(90), entry(89)], more: true });
    expect(ids(s)).toEqual([90, 89]);
  });

  it("changes only entries whose cards a turn changed", () => {
    const s = newestEntries(emptyActivity, { entries: [entry(2, { turn: 7, cards: [approval] }), entry(1)], more: false });
    expect(entryTurn(s, turn(7, { cards: [approval] }))).toBe(s);
    expect(entryTurn(s, turn(8, { cards: [{ ...approval, status: "done" }] }))).toBe(s);
    const next = entryTurn(s, turn(7, { cards: [{ ...approval, status: "cancelled" }] }));
    expect(next.entries[0]?.cards[0]?.status).toBe("cancelled");
    expect(next.entries[1]).toBe(s.entries[1]);
  });
});

describe("times", () => {
  it("label the day, tomorrow included, and add the time", () => {
    const now = new Date(2026, 9, 19, 12, 0);
    const at = (d: number, h = 6) => new Date(2026, 9, d, h, 30).toISOString();
    expect(dayLabel(at(19), now)).toBe("Today");
    expect(dayLabel(at(18), now)).toBe("Yesterday");
    expect(dayLabel(at(20), now)).toBe("Tomorrow");
    expect(dateTime(at(19), now).startsWith("Today ")).toBe(true);
    expect(dateTime("not a time")).toBe("");
  });
});
