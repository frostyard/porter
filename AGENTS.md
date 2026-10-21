# frostyard/porter

Porter is the owner's personal assistant inside
[lodge](https://github.com/frostyard/lodge), frostyard's tailnet-only web
shell. The owner talks to it in a chat panel beside whatever app they are
in, and it does scheduled work from prompts kept in their vault. It owns
none of the owner's data. It reads and acts only through the other apps'
MCP endpoints, with scoped tokens, and it thinks through lodge's inference
layer. Start at [docs/README.md](docs/README.md). The architecture is in
[docs/design/overview.md](docs/design/overview.md).

This file (`AGENTS.md`) is the CANONICAL agent instructions. `CLAUDE.md`,
`GEMINI.md` and `.github/copilot-instructions.md` are symlinks to it, and
`.claude/skills` symlinks to `.agents/skills/`
([ADR-0002](docs/adr/0002-agent-portable-instruction-surface.md)). Edit only
the canonical paths, and keep content tool-agnostic.

## Status

Pre-release, local only (not yet on GitHub). The repository holds lodge's
app scaffold (`porterd`, on lodge's kit `v0.1.0`) and the founding
documents, which are Proposed (2026-10-19). Nothing of Porter's own is
built yet. The order of work is the [roadmap](docs/plans/0001-roadmap.md).
Phase 1 is lodge's and the apps' work, in
[frostyard/lodge](https://github.com/frostyard/lodge),
[frostyard/frostnotes](https://github.com/frostyard/frostnotes) and
[frostyard/frostmail](https://github.com/frostyard/frostmail), which are
sibling checkouts at `../lodge`, `../frostnotes` and `../frostmail` when
present. Before writing Porter's counterpart of something those
repositories do, read their file.

## Map

- `cmd/porterd`: the daemon, with the app contract's socket rules as tests
  (kittest).
- `api/`, `internal/rpcserver`: its JSON-RPC API on `kit/jsonrpc`.
  `schema/rpc` is the contract, and `api/rpc.go` follows it by hand.
- `web/`: the web bundle on `@frostyard/lodge-client`.
- `packaging/lodge`: the manifest, the socket and service units, the system
  user, and `stage.sh`.
- `.github/workflows/ci.yml`: CI, the jobs of `make ci`.
  `internal/workflowcheck` tests that its actions are SHA-pinned with least
  privilege (frostyard/core ADR-0021).
- Planned (see the design doc):
  - the agent loop and tool registry;
  - the thread store;
  - the scheduler for routines;
  - the companion UI.

## Gates

- `make check` formats, vets and tests the daemon. Run it before calling any
  Go change complete.
- `make web-build` builds the bundle in an nsl machine. Never run pnpm, node
  or cargo on the host.
- `make ci` is `make verify`, `make race` and `make web-check` (nsl). CI runs
  the same targets on every pull request and push to `main`.
- `make lodge-package` stages Porter in lodge's install layout.

## Invariants (decided; binding from the first line of code)

- **Porter owns no data.** It reaches mail, calendar, tasks and notes only
  through each app's `/mcp/<id>` with that app's token, over lodge's agent
  socket. It never reads another app's files or database
  ([ADR-0003](docs/adr/0003-porter-is-a-lodge-app-that-owns-no-data.md)).
- **Porter drafts, and the owner sends.** No token carries a send or post
  right. Routines get only the unattended tools: reads, drafts, and notes
  in `Porter/` outside `Porter/Routines/`
  ([ADR-0004](docs/adr/0004-porter-drafts-and-the-owner-sends.md)).
- **Other people's words are data.** Tool results reach the model wrapped
  as content, never as instructions. Porter's page loads nothing remote.
- **Memory and routines live in the vault.** Porter writes memory only when
  asked, and never writes `Porter/Routines/`
  ([ADR-0005](docs/adr/0005-memory-and-routines-live-in-the-vault.md)).
- **Porter never runs as the account that owns the data.** It runs as user
  `frostporter`.

## Code conventions

- Go: `ctx` first; errors are lowercase and wrapped with `%w`; `slog` for
  logs; `encoding/json/v2`; no mutable package state; modern idioms
  (`t.Context()`, `t.Chdir`, `wg.Go`, `omitzero`, `b.Loop`). Tests use temp
  dirs; no sleeps for synchronization.
- API changes start in `schema/rpc`.
- Never log a prompt, a tool result or a token. Log the app, the tool, the
  status and the duration.

## Repository boundary

Never commit `build/`, `web/node_modules`, tokens, the owner's notes or mail,
or private host names. Tests use made-up data.
Use conventional commit subjects.

## Documentation rules

Docs live in `docs/` in four categories (frostyard/core ADR-0025).
**Every new doc starts from its category's `TEMPLATE.md`.**

- `docs/adr/`: why we decided. Immutable once Accepted. A reversal is a new
  ADR that marks the old one Superseded. A Proposed ADR may be edited in
  place.
- `docs/design/`: how it fits together. Living, and updated to match
  reality.
- `docs/specs/`: exact contracts. They change only alongside implementing
  code; a founding spec says when it binds.
- `docs/plans/`: the order of work. Phases with "Done when" outcomes.

Cross-linking is mandatory in both directions. An ADR links the design docs
and specs it shapes. A design doc links its ADRs, its specs and the roadmap
phase that builds it. A spec links its ADRs and its design doc. A plan links
what each phase implements. A new doc gets a line in
[docs/README.md](docs/README.md). Convert relative dates to absolute dates.
