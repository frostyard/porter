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

Pre-release, public on GitHub. Phases 0, 1, 2, 3 and 5 of the
[roadmap](docs/plans/0001-roadmap.md) are built: chat in the companion,
routines with Activity and Approvals, and reading from frostread. Phase 4,
the GitHub manager, waits for that app. The done-when checks of Phases 2
and 3 are still open. `main` at `d3db41b` is deployed on lodge's host
(2026-10-21); the roadmap's Deployments table records each deploy. porterd
is on lodge's kit `v0.3.0`, and the web bundle on `@frostyard/lodge-client`
`client/v0.3.2` and `@frostyard/lodge-ui` `ui/v0.6.0`. ADR-0007 is Accepted
(Brian, 2026-10-21), and ADRs 0001 to 0006 are Accepted (Brian,
2026-10-23); the design doc and the routines spec are Proposed.
Phase 1 was lodge's and the apps' work, in
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
- `internal/thread`: the one continuous thread
  ([ADR-0006](docs/adr/0006-one-continuous-thread.md)): its turns, their
  full-text index and the rolling summary, in porterd's own SQLite database.
- `internal/tools`: the tool registry over lodge's agent socket; each tool
  is marked by what it may do
  ([ADR-0004](docs/adr/0004-porter-drafts-and-the-owner-sends.md)).
- `internal/agent`: the agent loop: the system prompt, the owner's memory,
  the summary and the recent turns, the tools, and the reply's cards.
- `internal/service`: the thread domain over the agent and the store.
- `internal/routine`: the scheduler for routines, and Activity
  ([specs/routines.md](docs/specs/routines.md)).
- `internal/links`: the links in text that Porter writes into the vault.
- `internal/config`: `/etc/porter/porter.json` and the state directory.
- `web/`: the companion and the full app (the thread, cards, Activity and
  Approvals), on `@frostyard/lodge-client` and `@frostyard/lodge-ui`.
- `packaging/lodge`: the manifest, the socket and service units, the system
  user, and `stage.sh`.
- `.github/workflows/ci.yml`: CI, the jobs of `make ci`.
  `internal/workflowcheck` tests that its actions are SHA-pinned with least
  privilege (frostyard/core ADR-0021).

## Gates

- `make check` formats, vets and tests the daemon. Run it before calling any
  Go change complete.
- `make web-build` builds the bundle in an nsl machine. Never run pnpm, node
  or cargo on the host.
- `make ci` is `make verify`, `make race` and `make web-check` (nsl). CI runs
  the same targets on every pull request and push to `main`, plus its
  `vulncheck` job (`make vulncheck`: govulncheck, pinned in the Makefile; it
  needs the network, so no gate runs it). Keep it at 0 reachable findings.
- `make lodge-package` stages Porter in lodge's install layout.

## Invariants (decided; binding from the first line of code)

- **Porter owns no data.** It reaches mail, calendar, tasks, notes and reading (read-only,
  [ADR-0007](docs/adr/0007-porter-reads-frostread-read-only.md)) only
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
