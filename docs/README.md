# Documentation

Docs are split by the question they answer (frostyard/core ADR-0025):

| Directory | Question | Contents |
| --- | --- | --- |
| [adr/](adr/) | **Why** did we choose this? | Architecture Decision Records: immutable once accepted, superseded, never edited |
| [design/](design/) | **How** does it fit together? | Living documents describing the current architecture |
| [specs/](specs/) | **What exactly** is the contract? | Precise, testable interface definitions |
| [plans/](plans/) | **When/in what order** do we build? | Roadmaps and phase plans, updated as work lands |

Org-wide decisions that bind this repository are in
[org-adrs.md](org-adrs.md). Porter is an app behind
[lodge](https://github.com/frostyard/lodge), under frostyard/core's
[ADR-0057](https://github.com/frostyard/core/blob/main/docs/adr/0057-run-first-party-apps-behind-lodge.md).

## Index

### Decisions (ADRs)

- [0001: Record architecture decisions](adr/0001-record-architecture-decisions.md)
- [0002: Agent-portable instruction surface](adr/0002-agent-portable-instruction-surface.md)
- [0003: Porter is a lodge app that owns no data](adr/0003-porter-is-a-lodge-app-that-owns-no-data.md)
- [0004: Porter drafts, and the owner sends](adr/0004-porter-drafts-and-the-owner-sends.md)
- [0005: Memory and routines live in the vault](adr/0005-memory-and-routines-live-in-the-vault.md)
- [0006: One continuous thread](adr/0006-one-continuous-thread.md)

### Design

- [Overview](design/overview.md): porterd, the request to the model, the system prompt, the UI, what Porter reaches, operations

### Specs

- [Routines](specs/routines.md): a routine note's format, and how porterd runs it

### Plans

- [0001: Roadmap](plans/0001-roadmap.md): what lodge and the apps provide, chat in the companion, routines, the GitHub manager

## Conventions

- **New docs start from their category's `TEMPLATE.md`** (in each directory).
- A new decision gets a new ADR with the next number. If it reverses an old
  one, mark the old one `Superseded by NNNN` rather than editing it.
- Design docs are updated in place to always reflect reality.
- Specs change only alongside the code that implements them. The founding
  spec says when it binds.
- Cross-links between categories are mandatory in both directions. See the
  documentation rules in [AGENTS.md](../AGENTS.md) (CLAUDE.md and GEMINI.md
  are symlinks to it, ADR-0002).
- Adding a doc means adding it to the index above.
