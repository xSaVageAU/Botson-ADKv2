# Process Architecture: one core, NATS-only

This document explains how Botson runs as an OS process: what "the core"
is, the two NATS subject namespaces it exposes, and how a client finds a
running one. It's a deep-dive companion to [AGENTS.md](../AGENTS.md)'s
"Unified core architecture" section -- read that first for the short
version.

---

## 1. The problem this design solves

Originally, `botson tui`, `botson web`, and `botson discord` were three
fully independent programs, each building its own copy of the Gemini model
client, the agent registry, and the session-database connection from
scratch. A series of redesigns (2026-07) fixed the sharing problem (one
core process holds all of that state, everything else is a NATS client of
it) but a later revision still left the core with a TUI built directly
into the same binary -- including a fallback path where the TUI would
quietly become its own private, unregistered core if none was running.
That fallback made "is there one shared agent instance, or several" a
fact you had to reason about per-invocation.

The current design removes that ambiguity entirely: **`botson` ships
exactly one thing, `botson core`, and it never runs as anything else.**
There is no TUI, no tray, and no code path anywhere in this binary that
builds a second, unregistered copy of the agent runtime. Any consumer --
a Discord bot, a web UI, a terminal chat client -- is a *separate project*
that talks to the one core purely over NATS. If it's running, there's
exactly one agent instance; if it's not, there's zero.

---

## 2. The core concept: one process, two subject namespaces

**The core is `botson core`.** The process holds:

- the Gemini model client (`google.golang.org/genai` + `adk/v2/model/gemini`)
- the agent registry / loader (`internal/agent`)
- the session service (SQLite-backed, `internal/session`)
- the artifact service (`internal/artifact`)
- an embedded NATS server (`nats-server/v2`, in-process -- no external NATS server to install or run)

Every consumer talks to it over exactly two NATS subject namespaces,
built from the same underlying `launcher.Config` (agent loader, session
service, artifact service):

- **`adk.*`** -- the standard ADK REST/A2A surface (list-apps, sessions,
  running a turn, A2A JSON-RPC), fronted by an imported
  [`github.com/Savs-Agents/NATS-ADK-Proxy`](https://github.com/Savs-Agents/NATS-ADK-Proxy).
  That package runs a real `google.golang.org/adk/v2/cmd/launcher/prod`
  instance on a loopback port and reverse-proxies NATS traffic to it, so
  behavior always matches upstream ADK exactly -- this repo doesn't
  reimplement any of it. See that package's own README for the subject
  and wire-header contract.
- **`botson.*`** -- `internal/natsapi`, for the state that isn't part of
  stock ADK's API: settings (`botson.settings.*`), custom-agent CRUD
  (`botson.agents.*`), and dashboard-shaped session listing/inspection
  (`botson.sessions.*`, `botson.dashboard.*`). See
  `internal/natsapi/subjects.go` for the full subject table.

```
                     ┌─────────────────────────────────────┐
                     │         botson core (process)         │
                     │                                       │
                     │   Gemini model · agent registry ·     │
                     │   session DB · artifact store ·       │
                     │   embedded NATS server                │
                     │                                       │
                     │   adk.*     -> NATS-ADK-Proxy (imported) │
                     │   botson.*  -> internal/natsapi          │
                     └───────────────┬───────────────────────┘
                                      │  NATS, over 127.0.0.1
              ┌───────────────────────┼───────────────────────────┐
              │                       │                           │
     ┌────────▼────────┐    ┌─────────▼─────────┐      ┌──────────▼─────────┐
     │ a Discord bot     │    │ a web UI            │      │ anything else --   │
     │ (separate project, │    │ (separate project,  │      │ each its own NATS  │
     │  own NATS client)  │    │  own NATS client)    │      │  client, no import │
     └─────────────────────┘   └───────────────────────┘      │  of this module    │
                                                                 └─────────────────────┘
```

There is no exception to this. Unlike the design's earlier revision,
there is no case where a client embeds its own private core -- the only
way an agent instance exists is `botson core` (foreground) or
`botson core start` (detached), and both are always discoverable the same
way (§4).

---

## 3. Process inventory

| Command | What it is | Does it hold state? | Discoverable by other processes? |
|---|---|---|---|
| `botson core` | The core. Embedded NATS server + `adk.*`/`botson.*` subject handlers. | Yes -- the only one that does. | Yes, via `~/.botson/core.pid` (always, regardless of how it was launched). |
| `botson core start` / `stop` / `status` | Lifecycle wrapper: launches `botson core` as a detached background process, or asks a running one to stop / reports on it. | No (separate short-lived CLI invocation). | N/A -- manages the discoverable state above. |
| `botson setup install` | Writes `~/.botson/config.json` (Gemini API key, model, root agent). The one thing that has to stay local/direct-to-disk, since it must work before any core or NATS server exists. | No. | No. |

---

## 4. How a client finds (or announces) a core

Discovery is entirely file-based, through `internal/daemon` -- deliberately
simple: no network broadcast, no service registry, just a JSON file per
named process under `~/.botson/`.

**State file** (`~/.botson/core.pid`):
```json
{
  "pid": 12345,
  "port": 54321,
  "started_at": "2026-07-08T17:25:41Z",
  "meta": { "natsPort": "4222" }
}
```
- `pid` / `started_at` are informational (shown by `status`).
- `port` is a private, ephemeral **control channel** the process listens
  on (see below) -- not the NATS port.
- `meta.natsPort` lets a client find the real NATS port even if the core
  was started with a non-default `--port`.

**Liveness check**: `daemon.GetStatus`/`Stop` don't trust the PID alone
(stale files after a crash are common) -- they dial the control-channel
port with a short timeout. A successful connect means "alive"; failure
means the state is stale, and it's opportunistically deleted.

**Graceful stop**: sending the literal line `stop\n` to the control
channel invokes that process's own `context.CancelFunc`, so it shuts down
through its normal path (closing NATS listeners cleanly, etc.) rather than
being killed. This exists mainly because Windows has no equivalent of
sending SIGTERM to an arbitrary process -- the same mechanism is used on
Linux too, for consistency. `--force` skips all of this and just calls
`os.Process.Kill`.

**Who writes the state file**: `runCore` (`cmd/botson-core/cmd_core.go`)
always registers -- whether it was invoked as a plain foreground
`botson core`, as `core start`'s detached child, or under an external
process supervisor like systemd. There is no separate hidden "daemon
child" subcommand; the detached child `core start` spawns is literally
the same `core` subcommand a user would type by hand. This is what makes
a systemd-managed `botson core` just as discoverable as one started with
`core start`.

---

## 5. Lifecycle: `botson core`

```
botson core [--port 4222]                          # foreground, registers daemon state, blocks until Ctrl+C or `stop`
botson core start [--port 4222]                     # spawns a detached child running the line above, waits up to 5s for it to report ready
botson core status                                  # reads the state file + probes the control channel
botson core stop [--force]                          # graceful stop via control channel, or force-kill
```

Internally (`cmd/botson-core/cmd_core.go`), `runCore` does the registration
(state file + control listener) and then calls `runCoreServer`, which does
the actual work: build an embedded `*server.Server` (`nats-server/v2/server`)
on the given loopback port, `.Start()` it, wait for `ReadyForConnections`,
connect a `*nats.Conn` to it, then run two things concurrently against
that connection (via `errgroup`) until `ctx` is cancelled:

- `adkproxy.New(...).Run(ctx)` -- the imported NATS-ADK-Proxy, serving `adk.*`.
- `natsapi.Serve(ctx, nc, boot.Launcher)` -- serving `botson.*`.

Logs go to `~/.botson/logs/core.log` when detached via `start`; a
foreground run just prints to the current terminal.

---

## 6. Setting up your own persistent core

Since there's no TUI or other in-process fallback, a core has to be
started explicitly before any NATS consumer can talk to Botson. Two
straightforward options:

**Just use `core start` once:**
```bash
botson core start --port 4222
```
It stays up until `botson core stop` or a reboot.

**Or run it under a real service supervisor** (recommended for anything
you want to survive a reboot). Since `runCore` always registers daemon
state regardless of how it's launched, a plain foreground invocation
under systemd works with no special flags:
```ini
# /etc/systemd/system/botson.service
[Unit]
Description=Botson core
After=network.target

[Service]
ExecStart=/root/.botson/bin/botson core --port 4222
WorkingDirectory=/path/to/your/project
Restart=on-failure
User=youruser

[Install]
WantedBy=multi-user.target
```
```bash
systemctl enable --now botson
botson core status   # confirms it's visible the same way `core start` would leave it
```
`WorkingDirectory` here no longer has any effect on tool behavior (see
§7) -- it's kept as a reasonable default cwd for the process itself (logs,
core files, etc.), not the agent's workspace.

There's currently no bundled systemd unit file or install-time offer to
set one up -- see §8.

---

## 7. Workspace directory resolution

Every tool the agent runs (`readFile`, `writeFile`, `runCommand`, etc.)
resolves paths against a **configured workspace root**, not the core
process's `os.Getwd()` -- `AppConfig.WorkspaceRoot`
(`internal/config/config.go`), defaulting to `~/.botson/workspace` and
settable via `setup install` or the `botson.settings.set` NATS subject
(`workspaceRoot`, applies immediately, no restart needed). This is
independent of how or where the core process was launched, unlike the
launch-time-`os.Getwd()` behavior this replaced.

A session can override this default entirely, via `stateDelta` on
`POST /api/run` (`{"botson:cwd": "/any/absolute/path"}`) -- see
[docs/nats-api.md §3](./nats-api.md#setting-a-sessions-working-directory).
That override is **not sandboxed to WorkspaceRoot**; it can point anywhere
the core process can read/write. This is why the embedded NATS server
requires a token (§8) -- the token is what gates a consumer's ability to
point a session's file/command tools at an arbitrary host path, since the
path itself isn't restricted. The resolution logic lives in
`internal/tools/workspace.go` (`effectiveRoot`, `resolveWorkspacePath`).

---

## 8. Limitations and directions worth thinking about

These are open, not resolved -- flagged here specifically because they
seem like the more likely next places this design gets pushed on.

- **No supervision or auto-restart.** If the core crashes, nothing brings
  it back -- a systemd unit with `Restart=on-failure` (§6) is currently the
  only way to get that, and it's not offered or documented anywhere except
  this file.
- **Discovery and the NATS server are single-host.** Everything is
  `127.0.0.1`-only by convention, not by enforcement -- there's no
  bind-address restriction or TLS on the embedded NATS server (it does now
  require a token, §7/`docs/nats-api.md` §1, generated into
  `~/.botson/config.json`). Fine for a local dev tool; would need real
  work (bind-address enforcement, TLS) before "point a client at a core
  running somewhere else, over a network you don't fully trust" becomes a
  supported idea.
- **`adk.*` doesn't stream yet.** NATS-ADK-Proxy's REST passthrough for
  `run` is currently request/reply only (`run_sse`/A2A `message/stream`
  aren't implemented upstream in that package yet) -- a caller gets the
  full turn's events back at once rather than incrementally. Since
  streaming lives in the shared proxy package, Botson inherits it
  automatically whenever it lands there.
- **No log rotation.** `~/.botson/logs/core.log` grows forever for a
  long-lived core; nothing truncates or rotates it.
- **No standalone Discord/web project exists yet.** Botson's NATS API
  (`adk.*` + `botson.*`) is designed to support one, but building it is
  future work, not something this repo does.

---

## See also

- [AGENTS.md — "Unified core architecture"](../AGENTS.md) -- the condensed version.
- [docs/nats-api.md](./nats-api.md) -- the full NATS API reference for building your own consumer against a running core.
- [docs/sessions.md](./sessions.md) -- how session state/history is actually stored, independent of which process is serving it.
- [NATS-ADK-Proxy](https://github.com/Savs-Agents/NATS-ADK-Proxy) -- the imported package fronting the `adk.*` surface.
