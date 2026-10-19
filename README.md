# Porter

The owner's personal assistant inside
[lodge](https://github.com/frostyard/lodge). You talk to it in a chat panel
beside whatever app you are in. It reads your mail, calendar, tasks and
notes through their MCP endpoints, drafts but never sends, keeps its memory
and its scheduled prompts (routines) as notes in your vault, and thinks
through lodge's inference layer. Pre-release: the design is in
[docs/](docs/README.md), and the code is lodge's app scaffold so far.

lodge's `porter` app: the `porterd` daemon, on the app socket lodge
reaches it through, and its web bundle, which lodge serves at
`/app/porter/`. It follows lodge's
[app contract](https://github.com/frostyard/lodge/blob/main/docs/specs/app-contract.md),
on [lodge's kit](https://github.com/frostyard/lodge/tree/main/kit) and
[`@frostyard/lodge-client`](https://github.com/frostyard/lodge/tree/main/client).

| Path | What |
| --- | --- |
| `cmd/porterd` | the daemon, with the contract's socket rules as tests (kittest) |
| `api`, `internal/rpcserver` | its JSON-RPC API, on `kit/jsonrpc` |
| `schema/rpc` | the API's contract; `api/rpc.go` follows it by hand |
| `web` | the web bundle, on `@frostyard/lodge-client` |
| `packaging/lodge` | the manifest, the socket and service units, the system user, and `stage.sh` |

- `make check` formats, vets and tests the daemon.
- `make web-build` builds the bundle in an nsl machine.
- `make lodge-package` stages everything under `build/lodge-root` (or `LODGE_DESTDIR`).
