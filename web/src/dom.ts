// DOM helpers. The bundle builds every node with createElement and
// textContent; nothing it shows is parsed as HTML.

/** el creates an element with an optional class and text. */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined) e.textContent = text;
  return e;
}

const SVG = "http://www.w3.org/2000/svg";

/** icon draws a 24×24 stroked icon from path data. */
export function icon(paths: string[], label?: string): SVGSVGElement {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "18");
  svg.setAttribute("height", "18");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "2");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  if (label === undefined) svg.setAttribute("aria-hidden", "true");
  for (const d of paths) {
    const p = document.createElementNS(SVG, "path");
    p.setAttribute("d", d);
    svg.append(p);
  }
  return svg;
}

/**
 * reconcile makes parent's children exactly `nodes`, in order, moving only
 * what is out of place, so focus, selection and edits in kept nodes stay.
 */
export function reconcile(parent: Element, nodes: readonly Node[]): void {
  const keep = new Set(nodes);
  for (const child of [...parent.childNodes]) if (!keep.has(child)) child.remove();
  let cursor: ChildNode | null = parent.firstChild;
  for (const n of nodes) {
    if (cursor === n) {
      cursor = cursor.nextSibling;
      continue;
    }
    parent.insertBefore(n, cursor);
  }
}
