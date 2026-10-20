import { describe, expect, it, vi } from "vitest";
import { Code, RpcError } from "../src/api";
import { App } from "../src/app";
import { confirmParams } from "../src/cards";
import { card, FakeTransport, flush, turn } from "./fake";

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
