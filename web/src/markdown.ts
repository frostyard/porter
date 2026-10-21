// Markdown, rendered safely: marked's lexer only, and DOM nodes built from
// its tokens with createElement and textContent. Nothing is parsed as HTML,
// raw HTML shows as text, and images are never loaded: each is a link
// reading "image: <alt>". A link opens only if it is a lodge address (with
// lodge.open) or https: (in a new tab, its real host shown); anything else
// is plain text (ADR-0004: the page loads nothing remote, and links show
// their real destination).

import { Lexer, type MarkedToken, type Token, type Tokens } from "marked";
import { el } from "./dom";

/** What a rendered lodge address calls on a click. */
export interface LinkOpener {
  open(path: string): void;
}

/** Where a link's destination goes. */
export type LinkTarget = { kind: "lodge"; path: string } | { kind: "https"; url: URL } | { kind: "text" };

interface Ctx {
  links: LinkOpener;
  /** Inside a link, where another link or an image link would nest. */
  inLink: boolean;
}

const OPTIONS = { gfm: true, breaks: true } as const;

/** renderMarkdown renders src into a fragment. */
export function renderMarkdown(src: string, links: LinkOpener): DocumentFragment {
  const frag = document.createDocumentFragment();
  let tokens: Token[];
  try {
    tokens = Lexer.lex(src, OPTIONS);
  } catch {
    frag.append(el("p", undefined, src));
    return frag;
  }
  blocks(frag, tokens, { links, inLink: false });
  return frag;
}

// C0 controls and DEL, which no destination the bundle opens contains.
const CONTROL = /[\u0000-\u001f\u007f]/;

/**
 * classifyHref says where a link may go. A lodge address starts with one
 * "/" (not "//" or "/\", which browsers read as another host).
 */
export function classifyHref(href: string): LinkTarget {
  const h = href.trim();
  if (h === "" || CONTROL.test(h)) return { kind: "text" };
  if (h.startsWith("/")) {
    return h[1] === "/" || h[1] === "\\" ? { kind: "text" } : { kind: "lodge", path: h };
  }
  let url: URL;
  try {
    url = new URL(h);
  } catch {
    return { kind: "text" };
  }
  return url.protocol === "https:" && url.host !== "" ? { kind: "https", url } : { kind: "text" };
}

const NAMED = new Map<string, string>([
  ["amp", "&"],
  ["lt", "<"],
  ["gt", ">"],
  ["quot", '"'],
  ["apos", "'"],
  ["nbsp", " "],
  ["hellip", "…"],
  ["mdash", "—"],
  ["ndash", "–"],
  ["lsquo", "‘"],
  ["rsquo", "’"],
  ["ldquo", "“"],
  ["rdquo", "”"],
  ["middot", "·"],
  ["bull", "•"],
  ["times", "×"],
  ["rarr", "→"],
  ["larr", "←"],
  ["deg", "°"],
  ["copy", "©"],
  ["reg", "®"],
  ["trade", "™"],
  ["euro", "€"],
]);

/**
 * decodeEntities resolves character references, which marked leaves for a
 * browser's HTML parser: numeric ones, and the common named ones.
 */
export function decodeEntities(s: string): string {
  if (!s.includes("&")) return s;
  return s.replace(/&(#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[a-zA-Z][a-zA-Z0-9]{1,31});/g, (m, body: string) => {
    if (body.startsWith("#")) {
      const hex = body[1] === "x" || body[1] === "X";
      const cp = Number.parseInt(body.slice(hex ? 2 : 1), hex ? 16 : 10);
      return cp === 0 || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff) ? "�" : String.fromCodePoint(cp);
    }
    return NAMED.get(body) ?? m;
  });
}

/** showsHost reports whether a link's text already shows its real host, so "(host)" would only repeat it. */
function showsHost(text: string, url: URL): boolean {
  const t = text.trim().toLowerCase();
  if (t === url.host) return true;
  if (!t.startsWith("https://")) return false;
  try {
    const u = new URL(t);
    return u.host === url.host && u.username === "" && u.password === "";
  } catch {
    return false;
  }
}

function blocks(parent: Node, tokens: readonly Token[], ctx: Ctx): void {
  for (const t of tokens) block(parent, t, ctx);
}

function inlines(parent: Node, tokens: readonly Token[] | undefined, ctx: Ctx): void {
  if (tokens === undefined) return;
  for (const t of tokens) inline(parent, t, ctx);
}

function withInlines<E extends HTMLElement>(e: E, tokens: readonly Token[] | undefined, ctx: Ctx): E {
  inlines(e, tokens, ctx);
  return e;
}

function block(parent: Node, token: Token, ctx: Ctx): void {
  const t = token as MarkedToken;
  switch (t.type) {
    case "space":
    case "def":
      return;
    case "paragraph":
      parent.appendChild(withInlines(el("p"), t.tokens, ctx));
      return;
    case "heading": {
      const depth = Math.min(Math.max(t.depth, 1), 6) as 1 | 2 | 3 | 4 | 5 | 6;
      parent.appendChild(withInlines(el(`h${depth}`), t.tokens, ctx));
      return;
    }
    case "code": {
      const pre = el("pre");
      pre.appendChild(el("code", undefined, t.text));
      parent.appendChild(pre);
      return;
    }
    case "blockquote": {
      const q = el("blockquote");
      blocks(q, t.tokens, ctx);
      parent.appendChild(q);
      return;
    }
    case "hr":
      parent.appendChild(el("hr"));
      return;
    case "list": {
      const list = t.ordered ? el("ol") : el("ul");
      if (t.ordered && typeof t.start === "number" && t.start !== 1) list.setAttribute("start", String(t.start));
      for (const item of t.items) {
        const li = el("li", item.task ? "task" : undefined);
        blocks(li, item.tokens, ctx);
        list.appendChild(li);
      }
      parent.appendChild(list);
      return;
    }
    case "table":
      parent.appendChild(table(t, ctx));
      return;
    case "html":
      // Raw HTML is shown as the text it is.
      parent.appendChild(el("p", "raw", t.text.replace(/\n+$/, "")));
      return;
    case "text":
      // A tight list item's text: its inline tokens, unwrapped.
      if (t.tokens !== undefined) inlines(parent, t.tokens, ctx);
      else parent.appendChild(document.createTextNode(decodeEntities(t.text)));
      return;
    case "checkbox":
      parent.appendChild(checkbox(t.checked));
      return;
    default:
      inline(parent, token, ctx);
  }
}

function table(t: Tokens.Table, ctx: Ctx): HTMLElement {
  const wrap = el("div", "table");
  const tbl = el("table");
  const cell = (c: Tokens.TableCell, tag: "th" | "td") => {
    const e = withInlines(el(tag), c.tokens, ctx);
    if (c.align !== null) e.style.textAlign = c.align;
    return e;
  };
  const head = el("thead");
  const hr = el("tr");
  for (const c of t.header) hr.appendChild(cell(c, "th"));
  head.appendChild(hr);
  tbl.appendChild(head);
  const body = el("tbody");
  for (const row of t.rows) {
    const tr = el("tr");
    for (const c of row) tr.appendChild(cell(c, "td"));
    body.appendChild(tr);
  }
  tbl.appendChild(body);
  wrap.appendChild(tbl);
  return wrap;
}

function checkbox(checked: boolean): HTMLInputElement {
  const box = el("input");
  box.type = "checkbox";
  box.checked = checked;
  box.disabled = true;
  return box;
}

function inline(parent: Node, token: Token, ctx: Ctx): void {
  const t = token as MarkedToken;
  switch (t.type) {
    case "text":
      if (t.tokens !== undefined) inlines(parent, t.tokens, ctx);
      else parent.appendChild(document.createTextNode(t.escaped ? t.text : decodeEntities(t.text)));
      return;
    case "escape":
      parent.appendChild(document.createTextNode(t.text));
      return;
    case "strong":
      parent.appendChild(withInlines(el("strong"), t.tokens, ctx));
      return;
    case "em":
      parent.appendChild(withInlines(el("em"), t.tokens, ctx));
      return;
    case "del":
      parent.appendChild(withInlines(el("del"), t.tokens, ctx));
      return;
    case "codespan":
      parent.appendChild(el("code", undefined, t.text));
      return;
    case "br":
      parent.appendChild(el("br"));
      return;
    case "html":
      // Inline raw HTML, such as <b> or <img onerror=…>, is shown as text.
      parent.appendChild(document.createTextNode(t.raw));
      return;
    case "link":
      link(parent, t.autolink ? t.href : decodeEntities(t.href), ctx, (a, inner) => inlines(a, t.tokens, inner));
      return;
    case "image": {
      // Never loaded: a link to it, reading "image: <alt>".
      const alt = plainText(t.tokens).trim();
      const label = alt === "" ? "image" : `image: ${alt}`;
      link(parent, decodeEntities(t.href), ctx, (a) => a.appendChild(document.createTextNode(label)));
      return;
    }
    case "checkbox":
      parent.appendChild(checkbox(t.checked));
      return;
    default:
      parent.appendChild(document.createTextNode(token.raw));
  }
}

/** link renders a link's content into an anchor, or as plain text when it may not open. */
function link(parent: Node, href: string, ctx: Ctx, fill: (into: Node, inner: Ctx) => void): void {
  const inner = { ...ctx, inLink: true };
  // A link inside a link would nest anchors: its content is enough.
  if (ctx.inLink) {
    fill(parent, inner);
    return;
  }
  const target = classifyHref(href);
  switch (target.kind) {
    case "lodge": {
      const a = el("a", "lodge-link");
      a.setAttribute("href", target.path);
      a.title = target.path;
      fill(a, inner);
      a.addEventListener("click", (e) => {
        // A modified click opens the canonical lodge URL as the browser would.
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        ctx.links.open(target.path);
      });
      parent.appendChild(a);
      return;
    }
    case "https": {
      const a = el("a", "web-link");
      a.setAttribute("href", target.url.href);
      a.target = "_blank";
      a.rel = "noopener noreferrer";
      a.title = target.url.href;
      fill(a, inner);
      if (!showsHost(a.textContent ?? "", target.url)) a.appendChild(el("span", "host", ` (${target.url.host})`));
      parent.appendChild(a);
      return;
    }
    case "text":
      fill(parent, inner);
      return;
  }
}

/** markdownText is src's text without its markup, for a one-line snippet. */
export function markdownText(src: string): string {
  let tokens: Token[];
  try {
    tokens = Lexer.lex(src, OPTIONS);
  } catch {
    return src;
  }
  return blockText(tokens).replace(/\s+/g, " ").trim();
}

function blockText(tokens: readonly Token[]): string {
  const parts: string[] = [];
  for (const token of tokens) {
    const t = token as MarkedToken;
    switch (t.type) {
      case "space":
      case "def":
      case "hr":
        break;
      case "paragraph":
      case "heading":
        parts.push(plainText(t.tokens));
        break;
      case "text":
        parts.push(t.tokens !== undefined ? plainText(t.tokens) : decodeEntities(t.text));
        break;
      case "code":
        parts.push(t.text);
        break;
      case "blockquote":
        parts.push(blockText(t.tokens));
        break;
      case "list":
        for (const item of t.items) parts.push(blockText(item.tokens));
        break;
      case "table":
        for (const row of [t.header, ...t.rows]) for (const c of row) parts.push(plainText(c.tokens));
        break;
      case "checkbox":
        break;
      default:
        parts.push(token.raw);
    }
  }
  return parts.join(" ");
}

/** plainText is the text of inline tokens, as an image's alt. */
function plainText(tokens: readonly Token[] | undefined): string {
  if (tokens === undefined) return "";
  let s = "";
  for (const token of tokens) {
    const t = token as MarkedToken;
    switch (t.type) {
      case "text":
        s += t.tokens !== undefined ? plainText(t.tokens) : t.escaped ? t.text : decodeEntities(t.text);
        break;
      case "strong":
      case "em":
      case "del":
      case "link":
      case "image":
        s += plainText(t.tokens);
        break;
      case "codespan":
      case "escape":
        s += t.text;
        break;
      case "br":
        s += " ";
        break;
      default:
        s += token.raw;
    }
  }
  return s;
}
