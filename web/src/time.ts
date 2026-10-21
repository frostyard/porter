// Times as the bundle shows them, in the viewer's zone: a day label for the
// thread's dividers, and a short day and time everywhere else.

import { el } from "./dom";

/** localDate parses an RFC 3339 time, or returns null. */
export function localDate(at: string): Date | null {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** dayKey names the local day of a time, "" when it does not parse. */
export function dayKey(at: string): string {
  const d = localDate(at);
  return d === null ? "" : `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
}

/** dayLabel says which day a time is: Today, Yesterday, Tomorrow, or a short date. */
export function dayLabel(at: string, now = new Date()): string {
  const d = localDate(at);
  if (d === null) return "";
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((midnight(now) - midnight(d)) / 86_400_000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  if (days === -1) return "Tomorrow";
  const opts: Intl.DateTimeFormatOptions = { weekday: "short", month: "short", day: "numeric" };
  if (d.getFullYear() !== now.getFullYear()) opts.year = "numeric";
  return d.toLocaleDateString(undefined, opts);
}

/** dateTime is a time's day label and its hour and minute, such as "Today 6:30 AM". */
export function dateTime(at: string, now = new Date()): string {
  const d = localDate(at);
  if (d === null) return "";
  return `${dayLabel(at, now)} ${d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}`;
}

/** timeEl shows dateTime in a <time>, the full date and time as its title. */
export function timeEl(at: string, className?: string): HTMLTimeElement {
  const t = el("time", className, dateTime(at));
  const d = localDate(at);
  if (d !== null) {
    t.dateTime = d.toISOString();
    t.title = d.toLocaleString();
  }
  return t;
}
