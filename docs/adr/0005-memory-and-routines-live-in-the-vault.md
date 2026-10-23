# 0005 — Memory and routines live in the vault

- **Status:** Accepted (Brian, 2026-10-23)
- **Date:** 2026-10-19

## Context

Porter needs three kinds of lasting state beyond its conversation:
- **memory:** facts the owner asks it to keep ("Iris's group does Saturday
  dinners");
- **routines:** work it does on a schedule, such as a weekday morning brief
  and a Sunday weekly review;
- **its output:** the briefs and reviews themselves.

The owner keeps their life in a markdown vault (frostnotes, "files are the
truth"), reads it on every device, and writes daily notes there. They want
to change what Porter does on a schedule by editing a prompt, not by
changing code.

Memory and routines are exactly what a hostile message would try to plant.
A planted fact ("I approve all invoices") or a planted routine would steer
Porter long after the message is forgotten.

## Decision

- **Porter's folder is `Porter/` in the vault:**
  - `Porter/Memory.md` holds memory;
  - `Porter/Briefs/` and `Porter/Reviews/` hold routine output;
  - Porter may write anywhere under `Porter/` except `Porter/Routines/`.
- **A routine is a note in `Porter/Routines/`.** Frontmatter gives its
  schedule, where its output goes, and whether the model thinks. Its body is
  the prompt. The owner writes and edits routines in notes; Porter only
  reads them. The format is [specs/routines.md](../specs/routines.md). The
  morning brief and the weekly review are the first two routines, not code.
- **Memory is written only when the owner asks.** When Porter notices
  something worth keeping, it suggests it in one line, and the owner
  decides. Every request to the model carries `Porter/Memory.md` in full.
- **Capture appends to today's daily note.** In chat, "remember this" or
  "note that…" shows the text it would append, and appends it to today's
  daily note once confirmed. It never edits text that is already there.
- **Notes enforces all of this through Porter's token:**
  `vault=<id>;write=Porter/;except=Porter/Routines/;daily`
  ([frostnotes ADR-0028](https://github.com/frostyard/frostnotes/blob/main/docs/adr/0028-an-agents-scope-can-limit-writes-to-folders.md)).
  Porter reads the whole vault, writes only `Porter/` outside its routines,
  and may append to today's daily note.

## Consequences

- The owner sees and edits everything Porter keeps, on any device, with
  frostnotes' version history. A wrong memory is fixed by editing a note.
- Adding or changing a routine is editing a note. porterd rereads routines
  when they change, so a new routine needs no code and no restart.
- A routine is only as safe as its owner's prompt. Whatever it says, it runs
  with the unattended rights of [ADR-0004](0004-porter-drafts-and-the-owner-sends.md),
  and Porter cannot write one.
- Memory grows with use. Being read in full on every request costs tokens,
  and the owner prunes it like any note.
- Porter depends on notes' daily-note settings (folder, name, template) to
  find today's note.

## Alternatives considered

- **Routines and memory in Porter's database, edited in its UI:** that is
  safe as well, but hidden from the owner's vault, and invisible on devices
  outside lodge.
- **Coded jobs:** every change would need a release.
- **Capture into `Porter/Inbox.md`:** that is safer, but the owner works in
  daily notes. An append-only right, behind a confirmation, keeps it safe
  enough.
- **Memory that Porter writes on its own judgment:** that opens the door to
  memory poisoning from mail it reads.

## References

- Shapes: [specs/routines.md](../specs/routines.md), [design/overview.md](../design/overview.md)
- Builds on: [ADR-0003](0003-porter-is-a-lodge-app-that-owns-no-data.md), [ADR-0004](0004-porter-drafts-and-the-owner-sends.md)
