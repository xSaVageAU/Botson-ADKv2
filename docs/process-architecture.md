# Process Architecture: the unified core and its clients

This document explains how Botson actually runs as one or more OS processes: what "the core" is, how a client finds and talks to it, the difference between an explicitly-started shared core and a private embedded one, and how the TUI and tray fit into that picture. It's a deep-dive companion to [AGENTS.md](../AGENTS.md)'s "Unified core architecture" section — read that first for the short version; this document exists to make the whole design legible in one place, including the parts that are still rough edges, so it's easier to reason about how to improve it.

---

## 1. The problem this design solves

Originally, `botson tui`, `botson web`, and `botson discord` were three fully independent programs. Each one, on every invocation, built its own copy of the Gemini model client, the agent registry, and the session-database connection from scratch (`setupApp()` in `cmd/botson/bootstrap.go`). They only ever coordinated indirectly, through the same SQLite session database and `config.json` file on disk. There was no in-memory sharing and no single answer to "is Botson running right now."

The first redesign (2026-07) moved to a **unified core**: one process holds the Gemini model, the agent registry, and the session/artifact services in memory, and every interface becomes a client of that one process instead of bootstrapping its own copy. That core was `botson web`, and clients talked to it over HTTP (ADK's own REST/A2A launcher stack).

A second redesign (also 2026-07) replaced that HTTP surface with **NATS**. The motivation: make it trivial to build fully independent consuming microservices — a Discord bot, a web console — as separate projects, needing only a NATS client and the subject/wire-type contract in `internal/interface/natscore`, never an import of this module's own Go packages. The web console and Discord gateway that used to live in this repo (and talk to the core over HTTP, in Discord's case partly in-process) were removed as part of this pivot; they're expected to be rebuilt later as standalone NATS-consuming projects. This repo now ships the core plus exactly one interface built on it: the TUI.

---

## 2. The core concept: one process, many clients

**The core is `botson core`.** Concretely, the core process holds:

- the Gemini model client (`google.golang.org/genai` + `adk/v2/model/gemini`)
- the agent registry / loader (`internal/agent`)
- the session service (SQLite-backed, `internal/session`)
- the artifact service (`internal/artifact`)
- an embedded NATS server (`nats-server/v2`, in-process — no external NATS server to install or run)

Every client — today, just the TUI, but by design any future process — is a **thin client**: it holds none of the above itself. It talks to the core purely over NATS (`internal/interface/apiclient` for the TUI; a from-scratch NATS client for anything else), never by importing this module's internal packages.

```
                     ┌─────────────────────────────────────┐
                     │   core process ("botson core")        │
                     │                                       │
                     │   Gemini model · agent registry ·     │
                     │   session DB · artifact store ·       │
                     │   embedded NATS server                │
                     │                                       │
                     │   subjects (internal/interface/natscore):│
                     │     botson.agent.default              │
                     │     botson.session.create              │
                     │     botson.session.get                 │
                     │     botson.run          (streaming)    │
                     └───────────────┬───────────────────────┘
                                      │  NATS, over 127.0.0.1
              ┌───────────────────────┼───────────────────────────┐
              │                                                   │
     ┌────────▼────────┐                                ┌─────────▼─────────┐
     │ botson tui        │                                │ (future) a standalone│
     │ (apiclient.Client) │                                │ Discord/web project,  │
     └────────────────────┘                                │ its own NATS client   │
                                                             └───────────────────────┘
                                       Windows only:
                                 ┌─────────────────────┐
                                 │ botson tray          │
                                 │ (daemon.Start/Stop   │
                                 │  for the core process)│
                                 └─────────────────────┘
```

The one deliberate exception: **the TUI, when no core is already running, becomes its own private core** rather than reaching out over the network for one. That case is important enough to get its own section (§5.2) — it's the newest and least obvious part of this design.

---

## 3. Process inventory

| Command | What it is | Does it hold state? | Discoverable by other processes? |
|---|---|---|---|
| `botson core` | The core. Embedded NATS server + `internal/interface/natscore`'s subject handlers. | Yes — the only one that does. | Yes, via `~/.botsonv2/core.pid` (always, regardless of how it was launched — see §4). |
| `botson core start` / `stop` / `status` | Lifecycle wrapper: launches `botson core` as a detached background process, or asks a running one to stop / reports on it. | No (this is a separate short-lived CLI invocation). | N/A — it manages the discoverable state above. |
| `botson tui` | Thin client. Attaches to a running core if one exists; otherwise runs a **private, unregistered** core inside itself (§5.2). | Only in the fallback case, and only for its own lifetime. | No, even in the fallback case — deliberately invisible to everything else. |
| `botson tray` (Windows only) | GUI thin client + a client-side supervisor for the core's own `core start`/`stop`. | No. | Yes, via `~/.botsonv2/tray.pid`, same mechanism as `core`. |

---

## 4. How a client finds (or announces) a core

Discovery is entirely file-based, through `internal/daemon` — deliberately simple: no network broadcast, no service registry, just a JSON file per named process under `~/.botsonv2/`.

**State file** (`~/.botsonv2/<id>.pid`, currently only `core` and `tray` use one):
```json
{
  "pid": 12345,
  "port": 54321,
  "started_at": "2026-07-08T17:25:41Z",
  "meta": { "natsPort": "4222" }
}
```
- `pid` / `started_at` are informational (shown by `status`).
- `port` is a private, ephemeral **control channel** the process listens on (see below) — not the NATS port.
- `meta` is a free-form string map the daemon package itself doesn't interpret. The core's only use of it today is `meta.natsPort`, so a client can find the real NATS port even if the core was started with a non-default `--port`.

**Liveness check**: `daemon.GetStatus`/`Stop` don't trust the PID alone (stale files after a crash are common) — they dial the control-channel port with a short timeout. A successful connect means "alive"; failure means the state is stale, and it's opportunistically deleted.

**Graceful stop**: sending the literal line `stop\n` to the control channel invokes that process's own `context.CancelFunc`, so it shuts down through its normal path (closing NATS listeners cleanly, etc.) rather than being killed. This exists mainly because Windows has no equivalent of sending SIGTERM to an arbitrary process — the same mechanism is used on Linux too, for consistency, even though SIGTERM would otherwise work fine there. `--force` skips all of this and just calls `os.Process.Kill`.

**Who writes the state file**: `runCore` (`cmd/botson/cmd_core.go`) always registers — whether it was invoked as a plain foreground `botson core`, as `core start`'s detached child, or under an external process supervisor like systemd. There is no separate hidden "daemon child" subcommand; the detached child `core start` spawns is literally the same `core` subcommand a user would type by hand. This matters for §6 below: it's what makes a systemd-managed `botson core` just as discoverable as one started with `core start`.

**The one process that deliberately never writes this file**: the TUI's private embedded core (§5.2). That absence of a state file *is* the mechanism that keeps it private.

---

## 5. Lifecycles in detail

### 5.1 The core: `botson core`

```
botson core [--port 4222]                          # foreground, registers daemon state, blocks until Ctrl+C or `stop`
botson core start [--port 4222]                     # spawns a detached child running the line above, waits up to 5s for it to report ready
botson core status                                  # reads the state file + probes the control channel
botson core stop [--force]                          # graceful stop via control channel, or force-kill
```

Internally (`cmd/botson/cmd_core.go`), `runCore` does the registration (state file + control listener) and then calls `runCoreServer`, which does the actual work: build an embedded `*server.Server` (`nats-server/v2/server`) on the given loopback port, `.Start()` it, wait for `ReadyForConnections`, connect a `*nats.Conn` to it, and call `natscore.Serve(ctx, nc, boot.Launcher)` — which subscribes to every subject and blocks until `ctx` is cancelled, at which point the embedded server is shut down too. Logs go to `~/.botsonv2/logs/core.log` when detached via `start`; a foreground run just prints to the current terminal.

### 5.2 The TUI: attach if possible, otherwise become your own core

This is the newest and most subtle part of the design, and the part most recently fixed after getting it wrong once — worth walking through carefully.

```
runTUI
  └─ ensureCoreRunning(ctx)
       ├─ daemon.GetStatus("core", ...) says a core is running?
       │     └─ yes → return its natsPort; connect as a pure NATS client
       │
       └─ no core running
             ├─ --no-auto-start set? → fail with a clear error ("run `botson core start` first")
             └─ otherwise → startEmbeddedCore(ctx)
                    ├─ bind an ephemeral loopback port (net.Listen "127.0.0.1:0", read the port, close it)
                    ├─ run setupApp() if this process hasn't already (the `tui` subcommand
                    │  normally skips it, since as a thin client it shouldn't need to --
                    │  see PersistentPreRunE: noBootstrap)
                    ├─ go runCoreServer(ctx, port, quiet=true)   -- NO daemon.WriteState call
                    └─ poll the port until it accepts a raw TCP connection (≤5s), then return it
```

The critical property: **`startEmbeddedCore` never calls `daemon.WriteState`.** Nothing else on the system can discover this core exists — `botson core status` from another terminal correctly reports "not running," and there is no `.pid` file for anything else to find. When the TUI process exits, its embedded core goes with it; nothing is left running in the background.

**Why this exists at all, rather than always requiring an explicit `core start` first:** a bare `botson` (opening the TUI) is meant to just work on a machine with nothing set up yet, with no separate setup step. But it must not *also* leave a service running silently forever just because you happened to open a chat window once. The fix embeds the same core code directly into the TUI's own process instead of spawning a second one.

**Consequence worth being deliberate about:** if you run `botson tui` twice in a row with no core ever explicitly started, you get *two* independent embedded cores, each with its own in-memory agent/session state for that run (though both still read/write the same SQLite session database on disk, so session history itself isn't lost — only the in-memory model/agent-registry instances are duplicated). If you want multiple front-ends to share one truly live, warm process, you have to start that core explicitly first (`botson core start`, or a systemd unit — see §6) and then everything else attaches to it instead of embedding its own.

### 5.3 The tray (Windows only)

The tray is a GUI thin client plus a client-side supervisor for the core (`cmd/botson/tray_windows.go`):

- Its "Core" menu item wraps `daemon.Start`/`daemon.Stop` against the `core` daemon id — functionally identical to running `botson core start`/`stop` yourself.
- It polls every 3 seconds (`trayPollLoop`) to keep the menu label honest, since nothing pushes state changes to it.
- Closing the tray never stops the core — it's just another client of the same daemon state, with no special ownership over the process it's showing status for.

`trayWorkspaceDir()` resolves the working directory the tray hands to `daemon.Start` when it spawns the core: it prefers `config.AppConfig.WorkspaceDir` (set once by `setup install`, since the tray itself has no meaningful cwd when launched via a login autostart entry) and falls back to its own `os.Getwd()` only if that's unset.

---

## 6. Setting up your own persistent core

The whole point of §5.2's fix is that Botson never does this for you implicitly — if you want a core that's always up and shared across every client (every TUI session, and any future NATS-consuming project, all warm and sharing one in-memory agent/session state), you set that up explicitly, the same way you would for any other long-running service. Two straightforward options:

**Just use `core start` once:**
```bash
botson core start --port 4222
```
It stays up until `botson core stop` or a reboot. Every `botson tui` invocation from then on attaches to it instead of embedding its own.

**Or run it under a real service supervisor** (recommended for anything you want to survive a reboot). Since `runCore` always registers daemon state regardless of how it's launched, a plain foreground invocation under systemd works with no special flags:
```ini
# /etc/systemd/system/botson.service
[Unit]
Description=Botson core
After=network.target

[Service]
ExecStart=/root/.botsonv2/bin/botson core --port 4222
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
`WorkingDirectory` here plays the same role `os.Getwd()` does for a manually-launched `core start` — it's the workspace every tool call (readFile, runCommand, etc.) resolves against for that core's entire lifetime (see §7).

There's currently no bundled systemd unit file or install-time offer to set one up (Windows gets the tray-autostart equivalent via `setup install`; Linux/macOS don't have an analogous prompt yet) — see §8 for this as a concrete improvement idea.

---

## 7. Workspace directory resolution

Every tool the agent runs (`readFile`, `writeFile`, `runCommand`, etc.) resolves paths relative to *the core process's own working directory* — there's no per-session or per-request workspace concept. That directory is fixed for the entire lifetime of whichever process is acting as the core:

| How the core was started | Workspace used |
|---|---|
| `botson core` (foreground, manual) | Its own `os.Getwd()` at launch |
| `botson core start` | The *caller's* `os.Getwd()` at the moment `start` was run (explicitly threaded through `daemon.Start`'s `dir` parameter) |
| Systemd (or similar) | Whatever `WorkingDirectory=` (or equivalent) was configured |
| TUI's embedded core | Wherever the TUI itself was launched from — no extra step, since it's the same process, not a spawned child |
| Tray-launched core | `config.AppConfig.WorkspaceDir` if set (since tray itself has no meaningful cwd), else tray's own cwd |

**Known limitation, not solved by any of this**: you cannot change an *already-running* core's workspace. It's pinned for that process's life — restart it from a different directory (or under a different systemd unit / different `--port` core entirely) to point it elsewhere. True per-session or per-tool-call workspace switching would mean threading a workspace argument through `agent.Context` and every tool built on `os.Getwd()` today — a materially bigger change than anything in this design so far.

---

## 8. Limitations and directions worth thinking about

These are open, not resolved — flagged here specifically because they seem like the more likely next places this design gets pushed on.

- **No supervision or auto-restart.** If the core crashes, nothing brings it back — a systemd unit with `Restart=on-failure` (§6) is currently the only way to get that, and it's not offered or documented anywhere except this file. Worth deciding whether Botson should ship an actual unit file / install-time offer for Linux, mirroring what `setup install` already does for the Windows tray's login autostart.
- **One workspace per core, for its whole life (§7).** If you want to work on two different projects with the agent at once, that's two entirely separate cores (different ports, no shared state, no shared model/session-service instance) rather than one core juggling both. Whether that's actually a limitation depends on how "multi-project" a workflow you expect — it's the single biggest architectural constraint of this design.
- **Discovery and the NATS server are single-host and unauthenticated.** Everything is `127.0.0.1`-only by convention, not by enforcement — there's no bind-address restriction, auth token, or TLS on the embedded NATS server. Fine for a local dev tool; would need real work before "point a client at a core running somewhere else" becomes a supported idea rather than something that happens to work if you're careless with `--port` and firewalls.
- **The TUI's embedded-core fallback re-pays the full bootstrap cost every time.** Loading the agent registry, opening the session DB, constructing the Gemini client — all of it happens fresh for every `botson tui` invocation that doesn't find a core to attach to. For a single quick session this is invisible; for someone who opens/closes the TUI frequently without ever running `core start`, it's wasted, repeated startup latency that a warm shared core would avoid entirely.
- **Two independent `botson tui` invocations with no shared core silently duplicate state** (§5.2) rather than erroring or warning that you're not actually getting a shared session. There's no nudge anywhere suggesting "you're about to open a second, disconnected instance — did you mean to `core start` first?"
- **Tray status is poll-based (3s), not pushed.** A state change made elsewhere takes up to 3 seconds to reflect in the tray's menu label. Minor, but a real, noticeable staleness window.
- **No log rotation.** `~/.botsonv2/logs/{core,tray}.log` grow forever for a long-lived core; nothing truncates or rotates them.
- **Windows and Unix asymmetry.** The loopback control-channel design exists specifically to work around Windows having no equivalent of sending a Unix signal to an arbitrary process — it's used uniformly on both platforms for consistency, but it does mean a slightly heavier mechanism (a real TCP round-trip) is used everywhere a plain `SIGTERM` would otherwise suffice on Linux/macOS.
- **No standalone Discord/web project exists yet.** This repo's NATS API (`internal/interface/natscore`) is designed to support one, but building it is future work, not something this repo does.

---

## See also

- [AGENTS.md — "Unified core architecture"](../AGENTS.md) — the condensed version, plus how this connects to the HITL confirmation protocol, the import-direction rules, and the rest of the codebase's conventions.
- [docs/sessions.md](./sessions.md) — how session state/history is actually stored, independent of which process is serving it.
