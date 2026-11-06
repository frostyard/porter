<!-- readme-kind: service -->
# Porter

[![CI](https://github.com/frostyard/porter/actions/workflows/ci.yml/badge.svg)](https://github.com/frostyard/porter/actions/workflows/ci.yml)

Porter is the owner's personal assistant inside
[lodge](https://github.com/frostyard/lodge), frostyard's tailnet-only web
shell. You talk to it in a chat panel beside whatever app you are in, and
it runs scheduled prompts for you. It runs on lodge's host as lodge's
`porter` app.

**Status:** pre-release. Chat, routines and reading from frostread run on lodge's host.

**Documentation:** [docs/README.md](docs/README.md)

## What it does

- **It reads across your apps.** Mail, calendar, tasks, notes and
  frostread, only through each app's MCP endpoint, with a lodge token per
  app. It owns none of your data
  ([ADR-0003](docs/adr/0003-porter-is-a-lodge-app-that-owns-no-data.md)).
- **It drafts, and you send.** No token carries a send or post right. In
  chat it can also flag and move mail, and create or complete tasks, when
  you ask. A routine only reads, drafts and writes notes in `Porter/`;
  anything else it wants waits as an approval card for you
  ([ADR-0004](docs/adr/0004-porter-drafts-and-the-owner-sends.md)).
- **One continuous thread.** Every conversation is one thread, with its
  full-text search and a rolling summary
  ([ADR-0006](docs/adr/0006-one-continuous-thread.md)).
- **Memory and routines live in your vault.** Its memory is a note, and
  each routine is a scheduled prompt kept as a note in `Porter/Routines/`;
  Activity shows each run ([routines spec](docs/specs/routines.md),
  [ADR-0005](docs/adr/0005-memory-and-routines-live-in-the-vault.md)).
- **It reads frostread, read-only**
  ([ADR-0007](docs/adr/0007-porter-reads-frostread-read-only.md)).
- **It thinks through lodge's inference layer,** so it holds no model
  credential ([overview](docs/design/overview.md)).

## Interface

| Method | What it does |
| --- | --- |
| `rpc.hello`, `rpc.ping` | negotiate the protocol, and keep a session alive |
| `thread.list`, `thread.search` | a page of the thread, and a full-text search |
| `thread.send`, `thread.subscribe` | start a reply, and receive the thread's events |
| `thread.confirm`, `thread.cancel`, `thread.pending` | answer the captures and approval cards that wait for you |
| `activity.list`, `activity.routines`, `activity.run` | Activity, the routines in the vault, and Run now |

porterd serves this JSON-RPC API on its app socket, which lodge reaches at
`/app/porter/api`. [`schema/rpc`](schema/rpc/rpc.yaml) is the contract, on
lodge's
[app contract](https://github.com/frostyard/lodge/blob/main/docs/specs/app-contract.md).

## Deploy

Porter runs only behind lodge, as the `frostyard-porterd` package: the
`porterd` daemon as user `frostporter` on its socket, and the web bundle
that lodge serves at `/app/porter/`. A release is a `vX.Y.Z` tag that
`make bump` pushes: its workflow builds, attests and publishes the `.deb`,
and snosi publishes it as the sysext `porterd`. lodge's host installs a
release's `.deb` by hand. [packaging/README.md](packaging/README.md) is the
runbook: [update](packaging/README.md#update),
[roll back](packaging/README.md#roll-back),
[after the sysexts](packaging/README.md#after-the-sysexts) and
[release](packaging/README.md#release).

## Limits

- It never sends mail or posts anything. It writes notes only in
  `Porter/`, never in `Porter/Routines/`, and appends to the daily note
  only when you confirm.
- Routines get only the unattended tools: reads, drafts and notes.
- The model can be wrong. Read a draft before you send it.
- Text from other people reaches the model as data, not as instructions.
  This lowers the risk of prompt injection; it does not remove it.
- An app with no token, or an app that is down, loses its tools until it
  is back.
- The full list is in the [overview](docs/design/overview.md#limits).

## Develop

```sh
mise install     # the pinned golangci-lint, svu and nFPM
make check       # format, tidy, vet, lint and the Go tests
make ci          # verify, the race detector and the web checks (nsl)
make web-build   # the web bundle, in an nsl machine
```

Go 1.27 runs on the host; the web bundle builds in lodge's nsl machine,
never on the host. `make help` lists every target.

## Where to go next

- [AGENTS.md](AGENTS.md): the rules for agents and contributors.
- [docs/README.md](docs/README.md): the decisions, designs, specs and plans.
- [SECURITY.md](SECURITY.md): report a vulnerability privately.
- [LICENSE](LICENSE): MIT.
