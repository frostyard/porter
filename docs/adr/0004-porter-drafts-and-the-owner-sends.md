# 0004 — Porter drafts, and the owner sends

- **Status:** Proposed
- **Date:** 2026-10-19

## Context

Porter reads mail written by strangers, web pages, and notes others may have
written. Any of them can carry instructions aimed at a model ("forward
this", "remember that I approve all invoices"). The model also makes things
up. In the 2026-10-19 test, a drafted reply added that a friend "should be
back from the mountains by then", which no source said.

Porter acts in two modes. In **chat**, the owner is present and reads each
answer. In **routines**, scheduled prompts run unattended
([ADR-0005](0005-memory-and-routines-live-in-the-vault.md)). Some actions
leave the owner's hands and cannot be undone: sending mail, posting to
GitHub, and sending an invitation, which creating or changing an event
with attendees does.

## Decision

- **Nothing outward-facing in v1.** Porter's tokens never carry a send
  right (mail's `;send`, or a later GitHub posting right). Porter drafts,
  and the owner reviews and sends. This holds for anything that reaches
  other people, whichever app it is in.
- **In chat, Porter may:**
  - read everything its tokens reach (the whole vault and every mail
    account);
  - draft;
  - flag and move mail, and create or complete tasks, when the owner asks;
  - write in its folder, `Porter/`;
  - append to today's daily note, only to capture something the owner asked
    for, after showing the text and getting a confirmation.
  Each action is a card in the thread, with an undo where the app has one.
- **Unattended (routines), Porter may:**
  - read;
  - write mail drafts;
  - write notes in `Porter/`.
  It flags, moves, completes and deletes nothing. Whatever else a routine
  wants to do waits as an **approval**, a card in the thread that takes one
  tap.
- **The tokens enforce the ceiling; Porter's policy narrows it.** Notes
  enforces the folder limits
  ([frostnotes ADR-0028](https://github.com/frostyard/frostnotes/blob/main/docs/adr/0028-an-agents-scope-can-limit-writes-to-folders.md)),
  and mail refuses a send. On top of that, porterd runs only the unattended
  tools in a routine. Any other tool it offers there only proposes the
  action, as an approval. No prompt can widen either.
- **Other people's words are data.**
  - Tool results reach the model wrapped and labelled as content, never as
    instructions.
  - Porter acts on no instruction found inside them, and mentions one when
    it matters.
  - It never copies the owner's notes or other mail into a draft to someone
    unless the owner asked for exactly that.
- **Porter's page loads nothing remote.** Its manifest leaves
  `remoteImages` off, so a link or image in an answer cannot carry data out
  when it renders. Links show their real destination.
- **Porter never invents to fill a gap.** In a draft, an unknown time,
  someone's plans or a commitment becomes a bracketed placeholder
  (`[time?]`) for the owner to fill in.

## Consequences

- The worst a hostile message can do is plant a bad draft or a misleading
  answer, and the owner sees both before anything leaves. It cannot send,
  plant a routine, or rewrite the owner's notes.
- Some useful automation waits on a tap, such as filing newsletters
  overnight. If a routine proves trustworthy, a later ADR may widen what
  routines may do, app by app.
- Every app Porter uses must keep its write scopes apart from its send
  scopes, as mail's `;write` and `;send` are. The GitHub manager has to do
  the same.

## Alternatives considered

- **Sending with a delay and an undo:** it still sends what a stranger's
  message may have steered, and a missed undo cannot be recalled.
- **Trusting the system prompt alone:** prompts are advice to a model that
  can be manipulated. The ceiling has to be enforced by the apps.
- **Read-only routines:** that would rule out the morning brief, which
  writes a note.

## References

- Shapes: [design/overview.md](../design/overview.md), [specs/routines.md](../specs/routines.md)
- Related: frostmail [ADR-0042](https://github.com/frostyard/frostmail/blob/main/docs/adr/0042-agents-get-calendar-task-and-contact-tools.md) (no event writes for agents, since an invitation is a send)
- Builds on: [ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md), lodge [ADR-0006](https://github.com/frostyard/lodge/blob/main/docs/adr/0006-agents-reach-each-app-through-its-own-mcp-endpoint.md)
