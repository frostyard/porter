# 0006 — One continuous thread

- **Status:** Accepted (Brian, 2026-10-23)
- **Date:** 2026-10-19

## Context

The owner talks to Porter mostly in the companion, a chat panel that opens
beside whatever app they are in (lodge's ADR-0013), on a desktop and on
their phone. Chat products usually split talk into many conversations, and
the owner then has to choose one, name it and find it again. Porter is one
assistant for one person. The local model has a 262k-token context, but it
gets slower as the prompt grows.

## Decision

- **Porter has one thread,** shared by the companion and its full app, on
  every device. It never starts a "new chat".
- **Context chips mark the topic.** When the current app reports what is on
  screen (lodge-client's `context`: a note, a message), the thread shows it
  as a chip ("About: Plan.md") that the owner can dismiss. A change of chip
  is where a topic changes.
- **Routine runs and approvals appear in the thread** as single lines with
  links, such as "Your brief for Monday is ready". The thread is also where
  approvals are tapped.
- **Each request carries:**
  - the system prompt;
  - `Porter/Memory.md`;
  - a rolling summary of the thread before the recent turns;
  - the recent turns;
  - the current context.
  porterd rewrites the summary in the background as turns age out.
- **The whole thread is searchable,** by the owner in the app and by the
  model through a tool, so nothing summarized away is lost.

## Consequences

- The model's prompt stays small and quick however long the thread gets,
  and old detail comes back through search.
- A summary can drop or blur something. Search and memory are the
  remedies, and anything that must not be lost belongs in memory.
- The owner never manages conversations. The thread reads like one
  long-running exchange with dated breaks.

## Alternatives considered

- **Many conversations:** the owner would have to file and find them, and
  each starts cold.
- **A thread per context (per note or per message):** knowledge would be
  scattered, and anything spanning apps ("what did Iris ask, and is it on
  my calendar?") would have no home.
- **Sending the whole thread every time:** it gets slower each day, and
  eventually overflows the window.

## References

- Shapes: [design/overview.md](../design/overview.md)
- Builds on: [ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md), lodge [ADR-0013](https://github.com/frostyard/lodge/blob/main/docs/adr/0013-the-companion-opens-beside-the-current-app.md)
