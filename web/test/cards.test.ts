import { describe, expect, it, vi } from "vitest";
import { type Card, Code, RpcError, type Turn } from "../src/api";
import { App } from "../src/app";
import { argLines, confirmParams, renderCard } from "../src/cards";
import { card, deferred, FakeTransport, flush, turn } from "./fake";

const capture = card("c1", {
  kind: "capture",
  status: "pending",
  app: "notes",
  tool: "append_daily",
  summary: "Add to today's daily note",
  text: "Call Iris about the trip",
});

async function setup(cards = [card("t1", { url: "/notes/1/Projects/Plan.md" }), capture]) {
  const transport = new FakeTransport()
    .on("thread.subscribe", () => false)
    .on("thread.list", () => ({ turns: [turn(7, { text: "Here it is.", cards })], more: false }))
    .on("thread.confirm", (p) => turn(8, { role: "notice", text: `Added to [today's note](/notes/1/Daily/2026-10-19.md) (${p.card})` }))
    .on("thread.cancel", () => null);
  const open = vi.fn<(url: string) => void>();
  const root = document.createElement("div");
  document.body.replaceChildren(root);
  const app = new App({ root, transport, shell: { role: "companion", open } });
  await app.connected();
  return { app, root, transport, open };
}

const q = <E extends Element>(root: Element, sel: string) => root.querySelector<E>(sel) as E;

function button(root: Element, label: string): HTMLButtonElement {
  const b = [...root.querySelectorAll<HTMLButtonElement>(".card.capture button")].find((x) => x.textContent === label);
  if (b === undefined) throw new Error(`no ${label} button`);
  return b;
}

describe("capture cards", () => {
  it("confirm sends just the card when the text is unchanged", async () => {
    const { app, root, transport } = await setup();
    expect(q<HTMLTextAreaElement>(root, ".card.capture textarea").value).toBe("Call Iris about the trip");
    button(root, "Confirm").click();
    await flush();
    expect(transport.paramsOf("thread.confirm")).toStrictEqual([{ card: "c1" }]);
    // The outcome, and the notice turn confirm answered.
    expect(q(root, ".card.capture").classList.contains("done")).toBe(true);
    expect(q(root, ".card.capture textarea")).toBeNull();
    expect(q(root, ".card-outcome")?.textContent).toBe("Added to today's daily note.");
    expect(app.state.turns.map((t) => t.id)).toEqual([7, 8]);
    expect(q(root, ".turn.notice")?.textContent).toContain("Added to today's note");
  });

  it("confirm sends the owner's edit when the text changed", async () => {
    const { root, transport } = await setup();
    const area = q<HTMLTextAreaElement>(root, ".card.capture textarea");
    area.value = "Call Iris about the trip on Friday";
    area.dispatchEvent(new Event("input", { bubbles: true }));
    button(root, "Confirm").click();
    await flush();
    expect(transport.paramsOf("thread.confirm")).toStrictEqual([{ card: "c1", text: "Call Iris about the trip on Friday" }]);
    expect(q(root, ".capture-text")?.textContent).toBe("Call Iris about the trip on Friday");
  });

  it("does not confirm an empty edit", async () => {
    const { root, transport } = await setup();
    const area = q<HTMLTextAreaElement>(root, ".card.capture textarea");
    area.value = "   ";
    area.dispatchEvent(new Event("input", { bubbles: true }));
    expect(button(root, "Confirm").disabled).toBe(true);
    button(root, "Confirm").click();
    await flush();
    expect(transport.paramsOf("thread.confirm")).toEqual([]);
  });

  it("cancel sends the card and shows it cancelled", async () => {
    const { root, transport } = await setup();
    button(root, "Cancel").click();
    await flush();
    expect(transport.paramsOf("thread.cancel")).toStrictEqual([{ card: "c1" }]);
    expect(q(root, ".card-outcome")?.textContent).toBe("Cancelled.");
  });

  it("keeps the card pending and says why when confirm fails", async () => {
    const { root, transport } = await setup();
    transport.on("thread.confirm", () => {
      throw new RpcError(Code.unavailable, "notes is down");
    });
    button(root, "Confirm").click();
    await flush();
    expect(q(root, ".card.capture").classList.contains("pending")).toBe(true);
    expect(q<HTMLElement>(root, ".card-error").hidden).toBe(false);
    expect(q(root, ".card-error")?.textContent).toMatch(/isn't reachable/);
    expect(button(root, "Confirm").disabled).toBe(false);
  });

  it("keeps the owner's edit when the turn updates while the capture is pending", async () => {
    const { app, root } = await setup();
    const area = q<HTMLTextAreaElement>(root, ".card.capture textarea");
    area.value = "edited";
    area.dispatchEvent(new Event("input", { bubbles: true }));
    const revised = { ...capture, summary: "Add this to today's note" };
    app.event({ event: "turn", data: turn(7, { text: "Here it is, and more.", cards: [card("t1"), revised] }) });
    expect(q(root, ".card.capture .card-summary")?.textContent).toBe("Add this to today's note");
    expect(q<HTMLTextAreaElement>(root, ".card.capture textarea").value).toBe("edited");
  });
});

describe("tool cards", () => {
  it("open the url they carry with lodge.open", async () => {
    const { root, open } = await setup();
    const line = q<HTMLAnchorElement>(root, ".card.tool a.card-line");
    expect(line.textContent).toContain("notes");
    expect(line.textContent).toContain("Read Plan.md");
    line.click();
    expect(open).toHaveBeenCalledWith("/notes/1/Projects/Plan.md");
  });

  it("show a failure's detail, and no link without a url", async () => {
    const { root } = await setup([card("t2", { app: "mail", status: "failed", summary: "Search mail", detail: "mail is down" })]);
    expect(q(root, ".card.tool a")).toBeNull();
    expect(q(root, ".card.tool .card-status")?.textContent).toBe("Failed");
    expect(q(root, ".card.tool .card-detail")?.textContent).toBe("mail is down");
  });
});

describe("confirmParams", () => {
  it("carries the text only when it changed", () => {
    expect(confirmParams(capture, "Call Iris about the trip")).toStrictEqual({ card: "c1" });
    expect(confirmParams(capture, "x")).toStrictEqual({ card: "c1", text: "x" });
  });
});

const flag = card("a1", {
  kind: "approval",
  status: "pending",
  app: "mail",
  tool: "flag_message",
  summary: "Flag Iris's message about the trip",
  args: JSON.stringify({ message: "<m1@example.com>", flagged: true, note: `${"long ".repeat(60)}end` }),
});

function approvalButton(root: Element, label: string): HTMLButtonElement {
  const b = [...root.querySelectorAll<HTMLButtonElement>(".card.approval button")].find((x) => x.textContent === label);
  if (b === undefined) throw new Error(`no ${label} button`);
  return b;
}

describe("approval cards", () => {
  it("show what they would do, with Approve and Dismiss while pending", async () => {
    const { root } = await setup([flag]);
    const c = q(root, ".card.approval.pending");
    expect(c.querySelector(".card-app")?.textContent).toBe("mail");
    expect(c.querySelector(".card-summary")?.textContent).toBe("Flag Iris's message about the trip");
    expect(c.querySelector(".card-tool")?.textContent).toBe("flag_message");
    const keys = [...c.querySelectorAll(".card-args dt")].map((x) => x.textContent);
    const values = [...c.querySelectorAll<HTMLElement>(".card-args dd")];
    expect(keys).toEqual(["message", "flagged", "note"]);
    expect(values[0]?.textContent).toBe("<m1@example.com>");
    expect(values[1]?.textContent).toBe("true");
    expect(values[2]?.textContent?.endsWith("…")).toBe(true);
    expect(values[2]?.title.endsWith("end")).toBe(true);
    expect([...c.querySelectorAll("button")].map((b) => b.textContent)).toEqual(["Approve", "Dismiss"]);
  });

  it("show their outcome once done, failed or dismissed", async () => {
    const { root, open } = await setup([
      { ...flag, id: "a1", status: "done", url: "/mail/inbox/m1" },
      { ...flag, id: "a2", status: "failed", detail: "mail is down" },
      { ...flag, id: "a3", status: "cancelled" },
      { ...flag, id: "a4", status: "done" },
    ]);
    expect(root.querySelectorAll(".card.approval button")).toHaveLength(0);
    const outcome = (id: string) => q(root, `[data-card="${id}"] .card-outcome`);
    expect(outcome("a1").textContent).toBe("Approved. Open");
    (outcome("a1").querySelector("a.card-link") as HTMLAnchorElement).click();
    expect(open).toHaveBeenCalledWith("/mail/inbox/m1");
    expect(outcome("a2").textContent).toBe("Failed: mail is down");
    expect(outcome("a2").classList.contains("failed")).toBe(true);
    expect(outcome("a3").textContent).toBe("Dismissed.");
    expect(outcome("a4").textContent).toBe("Approved.");
  });

  it("show no arguments when args is absent or not a JSON object", async () => {
    const { root } = await setup([
      { ...flag, id: "a1", args: undefined },
      { ...flag, id: "a2", args: "{not json" },
    ]);
    expect(root.querySelectorAll(".card-args")).toHaveLength(0);
  });

  it("approve calls thread.confirm, with both buttons disabled until it answers", async () => {
    const { app, root, transport } = await setup([flag]);
    const answer = deferred<Turn>();
    transport.on("thread.confirm", () => answer.promise);
    approvalButton(root, "Approve").click();
    expect(transport.paramsOf("thread.confirm")).toStrictEqual([{ card: "a1" }]);
    expect(approvalButton(root, "Approve").disabled).toBe(true);
    expect(approvalButton(root, "Dismiss").disabled).toBe(true);
    answer.resolve(turn(8, { role: "notice", text: "Flagged Iris's message." }));
    await flush();
    expect(q(root, ".card.approval").classList.contains("done")).toBe(true);
    expect(q(root, ".card.approval .card-outcome").textContent).toBe("Approved.");
    expect(app.state.turns.map((t) => t.id)).toEqual([7, 8]);
  });

  it("dismiss calls thread.cancel, with both buttons disabled until it answers", async () => {
    const { root, transport } = await setup([flag]);
    const answer = deferred<null>();
    transport.on("thread.cancel", () => answer.promise);
    approvalButton(root, "Dismiss").click();
    expect(transport.paramsOf("thread.cancel")).toStrictEqual([{ card: "a1" }]);
    expect(approvalButton(root, "Approve").disabled).toBe(true);
    expect(approvalButton(root, "Dismiss").disabled).toBe(true);
    answer.resolve(null);
    await flush();
    expect(q(root, ".card.approval .card-outcome").textContent).toBe("Dismissed.");
  });

  it("stay pending and say why when approving fails", async () => {
    const { root, transport } = await setup([flag]);
    transport.on("thread.confirm", () => {
      throw new RpcError(Code.unavailable, "mail is down");
    });
    approvalButton(root, "Approve").click();
    await flush();
    expect(q(root, ".card.approval").classList.contains("pending")).toBe(true);
    expect(q<HTMLElement>(root, ".card.approval .card-error").hidden).toBe(false);
    expect(q(root, ".card.approval .card-error").textContent).toBe("Mail isn't reachable right now. Try again in a moment.");
    expect(approvalButton(root, "Approve").disabled).toBe(false);
    expect(approvalButton(root, "Dismiss").disabled).toBe(false);
  });

  it("keep the failure a turn event reported before confirm answered", async () => {
    const { app, root, transport } = await setup([flag]);
    transport.on("thread.confirm", () => {
      const failed: Card = { ...flag, status: "failed", detail: "no such message" };
      app.event({ event: "turn", data: turn(7, { text: "Here it is.", cards: [failed] }) });
      return turn(8, { role: "notice", text: "Couldn't flag it." });
    });
    approvalButton(root, "Approve").click();
    await flush();
    expect(q(root, ".card.approval").classList.contains("failed")).toBe(true);
    expect(q(root, ".card.approval .card-outcome").textContent).toBe("Failed: no such message");
  });

  it("read-only, say pending approval instead of offering buttons", () => {
    const host = { open: vi.fn(), confirm: vi.fn(), cancel: vi.fn() };
    const c = renderCard(flag, host, undefined, true);
    expect(c.querySelectorAll("button")).toHaveLength(0);
    expect(c.querySelector(".card-outcome")?.textContent).toBe("Pending approval.");
    const cap = renderCard(capture, host, undefined, true);
    expect(cap.querySelector("textarea")).toBeNull();
    expect(cap.querySelector(".card-outcome")?.textContent).toBe("Pending confirmation.");
  });
});

describe("notice turns", () => {
  it("show their cards, as a routine's run reports them", async () => {
    const cards: Card[] = [card("t1", { app: "notes", summary: "Wrote Porter/Briefs/2026-10-19.md" }), flag];
    const transport = new FakeTransport()
      .on("thread.subscribe", () => false)
      .on("thread.list", () => ({ turns: [turn(9, { role: "notice", text: "Morning brief ran.", cards })], more: false }));
    const root = document.createElement("div");
    document.body.replaceChildren(root);
    const app = new App({ root, transport, shell: { role: "companion", open: vi.fn() } });
    await app.connected();
    const notice = q(root, ".turn.notice");
    expect(notice.classList.contains("with-cards")).toBe(true);
    expect(notice.querySelector(".card.tool .card-summary")?.textContent).toBe("Wrote Porter/Briefs/2026-10-19.md");
    expect(notice.querySelector(".card.approval .card-actions")).not.toBeNull();
    app.event({ event: "turn", data: turn(9, { role: "notice", text: "Morning brief ran.", cards: [] }) });
    expect(q(root, ".turn.notice").classList.contains("with-cards")).toBe(false);
  });
});

describe("argLines", () => {
  it("reads a JSON object's keys and one-line values, cutting long ones", () => {
    expect(argLines('{"to":"iris@example.com","n":3,"tags":["a","b"],"x":null,"body":"two\\n  lines"}')).toStrictEqual([
      { key: "to", value: "iris@example.com", full: "iris@example.com" },
      { key: "n", value: "3", full: "3" },
      { key: "tags", value: '["a","b"]', full: '["a","b"]' },
      { key: "x", value: "null", full: "null" },
      { key: "body", value: "two lines", full: "two lines" },
    ]);
    const cut = argLines(JSON.stringify({ s: "abcdefghij" }), 5);
    expect(cut).toStrictEqual([{ key: "s", value: "abcd…", full: "abcdefghij" }]);
  });

  it("reads anything but a JSON object as no lines", () => {
    for (const args of [undefined, "", "{", "[1,2]", '"text"', "42", "null"]) expect(argLines(args)).toEqual([]);
  });
});
