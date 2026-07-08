# Process Architecture: the unified core and its clients

This document explains how Botson actually runs as one or more OS processes: what "the core" is, how a client finds and talks to it, the difference between an explicitly-started shared core and a private embedded one, and how each interface (TUI, web console, Discord, tray) fits into that picture. It's a deep-dive companion to [AGENTS.md](../AGENTS.md)'s "Unified core architecture" section — read that first for the short version; this document exists to make the whole design legible in one place, including the parts that are still rough edges, so it's easier to reason about how to improve it.

---

## 1. The problem this design solves

Originally, `botson tui`, `botson web`, and `botson discord` were three fully independent programs. Each one, on every invocation, built its own copy of the Gemini model client, the agent registry, and the session-database connection from scratch (`setupApp()` in `cmd/botson/bootstrap.go`). They only ever coordinated indirectly, through the same SQLite session database and `config.json` file on disk. There was no in-memory sharing, no single answer to "is Botson running right now," and — the bug that originally forced this redesign — no explicit notion of a working directory: a background process could silently inherit whatever directory happened to be current at the moment it was spawned, which is especially broken for something like a tray icon launched at login with no meaningful directory of its own.

The redesign (2026-07) moved to a **unified core**: one process holds the Gemini model, the agent registry, and the session/artifact services in memory, and every interface becomes a client of that one process instead of bootstrapping its own copy. This is also what makes "let the agent turn Discord on for the user" a cheap in-process function call instead of spawning a whole second program.

---

## 2. The core concept: one process, many clients

**The core is just `botson web`.** It already ran ADK's full REST API (`/api/*` — session CRUD, `run_sse` streaming chat) and the web console, so nothing new had to be invented; it was simply promoted to "the one process that matters." Concretely, the core process holds:

- the Gemini model client (`google.golang.org/genai` + `adk/v2/model/gemini`)
- the agent registry / loader (`core/agent`)
- the session service (SQLite-backed, `core/session`)
- the artifact service (`core/artifact`)
- optionally, the Discord gateway (see §5.2) — a togglable feature of the core, not a separate program

Every other interface — the TUI, the CLI's `discord start/stop/status`, the Windows tray — is a **thin client**: it holds none of the above itself. It just makes HTTP calls to a core process over loopback (`core/interface/apiclient`), the same way a browser talks to the web console.

```
                     ┌─────────────────────────────────────┐
                     │   core process ("botson web")        │
                     │                                       │
                     │   Gemini model · agent registry ·     │
                     │   session DB · artifact store ·       │
                     │   Discord gateway (optional, toggled) │
                     │                                       │
                     │   serves:                             │
                     │     /api/*          (ADK's REST API)  │
                     │     /botson/api/*   (dashboard, agent │
                     │                      CRUD, Discord    │
                     │                      toggle, config)  │
                     │     /botson/        (web console SPA) │
                     └───────────────┬───────────────────────┘
                                      │  HTTP / SSE, over 127.0.0.1
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
     ┌────────▼────────┐   ┌──────────▼─────────┐   ┌─────────▼─────────┐
     │ botson tui        │   │ browser (console)   │   │ botson discord      │
     │ (apiclient.Client) │   │ (chat.js / fetch)   │   │ start/stop/status   │
     └────────────────────┘   └─────────────────────┘   │ (apiclient.Client)  │
                                                          └─────────────────────┘
                                       Windows only:
                                 ┌─────────────────────┐
                                 │ botson tray          │
                                 │ (apiclient.Client +  │
                                 │  daemon.Start/Stop   │
                                 │  for the web process)│
                                 └─────────────────────┘
```

The one deliberate exception: **the TUI, when no core is already running, becomes its own private core** rather than reaching out over the network for one. That case is important enough to get its own section (§5.3) — it's the newest and least obvious part of this design.

---

## 3. Process inventory

| Command | What it is | Does it hold state? | Discoverable by other processes? |
|---|---|---|---|
| `botson web` | The core. REST/A2A APIs + console + Discord toggle. | Yes — the only one that does. | Yes, via `~/.botsonv2/web.pid` (always, regardless of how it was launched — see §4). |
| `botson web start` / `stop` / `status` | Lifecycle wrapper: launches `botson web` as a detached background process, or asks a running one to stop / reports on it. | No (this is a separate short-lived CLI invocation). | N/A — it manages the discoverable state above. |
| `botson tui` | Thin client. Attaches to a running core if one exists; otherwise runs a **private, unregistered** core inside itself (§5.3). | Only in the fallback case, and only for its own lifetime. | No, even in the fallback case — deliberately invisible to everything else. |
| `botson discord` (no subcommand) | A fully standalone Discord gateway process, independent of any core. For isolating Discord onto its own machine/process if you want that. | Yes, but doesn't share it with anything. | No — writes no daemon state at all. |
| `botson discord start` / `stop` / `status` | Thin client. Calls a running core's `/botson/api/discord/*` to toggle/query its in-process Discord gateway. | No. | N/A |
| `botson tray` (Windows only) | GUI thin client + a client-side supervisor for the core's own `web start`/`stop`. | No. | Yes, via `~/.botsonv2/tray.pid`, same mechanism as `web`. |
| `botson-discord`, `botson-adk` | Separate minimal binaries (`cmd/botson-discord`, `cmd/botson-adk`), each self-contained, for narrower deployments. Outside the unified-core picture entirely. | Yes, independently. | No. |

---

## 4. How a client finds (or announces) a core

Discovery is entirely file-based, through `core/daemon` — deliberately simple: no network broadcast, no service registry, just a JSON file per named process under `~/.botsonv2/`.

**State file** (`~/.botsonv2/<id>.pid`, currently only `web` and `tray` use one):
```json
{
  "pid": 12345,
  "port": 54321,
  "started_at": "2026-07-08T17:25:41Z",
  "meta": { "apiPort": "8080" }
}
```
- `pid` / `started_at` are informational (shown by `status`).
- `port` is a private, ephemeral **control channel** the process listens on (see below) — not the REST API port.
- `meta` is a free-form string map the daemon package itself doesn't interpret. The core's only use of it today is `meta.apiPort`, so a client can find the real REST API port even if the core was started with a non-default `--port`.

**Liveness check**: `daemon.GetStatus`/`Stop` don't trust the PID alone (stale files after a crash are common) — they dial the control-channel port with a short timeout. A successful connect means "alive"; failure means the state is stale, and it's opportunistically deleted.

**Graceful stop**: sending the literal line `stop\n` to the control channel invokes that process's own `context.CancelFunc`, so it shuts down through its normal path (closing the Discord gateway cleanly, stopping listeners, etc.) rather than being killed. This exists mainly because Windows has no equivalent of sending SIGTERM to an arbitrary process — the same mechanism is used on Linux too, for consistency, even though SIGTERM would otherwise work fine there. `--force` skips all of this and just calls `os.Process.Kill`.

**Who writes the state file**: as of the most recent revision, `runWeb` (`cmd/botson/cmd_web.go`) always registers — whether it was invoked as a plain foreground `botson web`, as `web start`'s detached child, or under an external process supervisor like systemd. There is no longer a separate hidden "daemon child" subcommand; the detached child `web start` spawns is literally the same `web` subcommand a user would type by hand. This matters for §6 below: it's what makes a systemd-managed `botson web` just as discoverable as one started with `web start`.

**The one process that deliberately never writes this file**: the TUI's private embedded core (§5.3). That absence of a state file *is* the mechanism that keeps it private.

---

## 5. Lifecycles in detail

### 5.1 The core: `botson web`

```
botson web [--port 8080] [--otel_to_cloud]        # foreground, registers daemon state, blocks until Ctrl+C or `stop`
botson web start [--port 8080]                     # spawns a detached child running the line above, waits up to 5s for it to report ready
botson web status                                  # reads the state file + probes the control channel
botson web stop [--force]                          # graceful stop via control channel, or force-kill
```

Internally (`cmd/botson/cmd_web.go`), `runWeb` does the registration (state file + control listener) and then calls `runCoreServer`, which does the actual work: `discord.InitCore(...)` (wiring the Discord singleton so it has what it needs if later toggled on) and launching ADK's REST/A2A/console sublaunchers via `universal.NewLauncher(...)`. Logs go to `~/.botsonv2/logs/web.log` when detached via `start`; a foreground run just prints to the current terminal.

### 5.2 Discord: in-process toggle, or fully standalone

Discord is **not** a background daemon in its own right anymore. `core/interface/discord/singleton.go` holds a package-level `*Gateway` behind a mutex — starting/stopping it is just spinning a goroutine and a discordgo session up or down *inside whatever process called `discord.InitCore`* (i.e., the core). This is what lets the agent itself flip Discord on/off mid-conversation via the `toggleDiscord` tool, with no process spawning involved at all.

```
botson discord start / status / stop     # calls the running core's /botson/api/discord/* over HTTP; errors clearly if no core is running
botson discord                           # ignores all of the above -- a fully standalone, foreground, core-independent process
```

The standalone form exists for anyone who genuinely wants Discord isolated — a different machine, a different lifecycle, whatever. It does its own full bootstrap and writes no daemon state, so it's invisible to `setup status` and to the toggle commands above; it's a deliberate escape hatch, not an oversight.

### 5.3 The TUI: attach if possible, otherwise become your own core

This is the newest and most subtle part of the design, and the part most recently fixed after getting it wrong once — worth walking through carefully.

```
runTUI
  └─ ensureCoreRunning(ctx)
       ├─ daemon.GetStatus("web", ...) says a core is running?
       │     └─ yes → return its apiPort; attach as a pure HTTP/SSE client
       │
       └─ no core running
             ├─ --no-auto-start set? → fail with a clear error ("run `botson web start` first")
             └─ otherwise → startEmbeddedCore(ctx)
                    ├─ bind an ephemeral loopback port (net.Listen "127.0.0.1:0", read the port, close it)
                    ├─ run setupApp() if this process hasn't already (the `tui` subcommand
                    │  normally skips it, since as a thin client it shouldn't need to --
                    │  see PersistentPreRunE: noBootstrap)
                    ├─ silence the stdlib `log` package output (it would otherwise corrupt
                    │  the TUI's alt-screen mid-render)
                    ├─ go runCoreServer(ctx, port, otelToCloud=false, quiet=true)   -- NO daemon.WriteState call
                    └─ poll the port until it accepts connections (≤5s), then return it
```

The critical property: **`startEmbeddedCore` never calls `daemon.WriteState`.** Nothing else on the system can discover this core exists — `botson web status` from another terminal correctly reports "not running," and there is no `.pid` file for anything else to find. When the TUI process exits, its embedded core goes with it; nothing is left running in the background.

**Why this exists at all, rather than always requiring an explicit `web start` first:** a bare `botson` (opening the TUI) is meant to just work on a machine with nothing set up yet, with no separate setup step. But it must not *also* leave a service running silently forever just because you happened to open a chat window once — that was a real bug in an earlier version of this design (an auto-start path used to call `daemon.Start`, spawning a genuine detached background process indistinguishable from one made via `web start`). The fix embeds the same core code directly into the TUI's own process instead of spawning a second one.

**Consequence worth being deliberate about:** if you run `botson tui` twice in a row with no core ever explicitly started, you get *two* independent embedded cores, each with its own in-memory agent/session state for that run (though both still read/write the same SQLite session database on disk, so session history itself isn't lost — only the in-memory model/agent-registry instances are duplicated). If you want multiple front-ends to share one truly live, warm process, you have to start that core explicitly first (`botson web start`, or a systemd unit — see §6) and then everything else attaches to it instead of embedding its own.

### 5.4 The tray (Windows only)

The tray is a GUI thin client plus a client-side supervisor for the *web* core specifically (`cmd/botson/tray_windows.go`):

- Its "Web" menu item wraps `daemon.Start`/`daemon.Stop` against the `web` daemon id — functionally identical to running `botson web start`/`stop` yourself.
- Its "Discord" menu item is a pure HTTP toggle (`discordCoreClient()`, shared with `botson discord start/stop/status`) against whatever core is currently running — it does **not** manage a `discord` daemon (there isn't one anymore).
- It polls both every 3 seconds (`trayPollLoop`) to keep menu labels honest, since nothing pushes state changes to it.
- Closing the tray never stops the core — it's just another client of the same daemon state / core API, with no special ownership over the process it's showing status for.

`trayWorkspaceDir()` resolves the working directory the tray hands to `daemon.Start` when it spawns the web core: it prefers `config.AppConfig.WorkspaceDir` (set once by `setup install`, since the tray itself has no meaningful cwd when launched via a login autostart entry) and falls back to its own `os.Getwd()` only if that's unset.

---

## 6. Setting up your own persistent core

The whole point of §5.3's fix is that Botson never does this for you implicitly — if you want a core that's always up and shared across every client (TUI sessions, Discord, the web console, all warm and sharing one in-memory agent/session state), you set that up explicitly, the same way you would for any other long-running service. Two straightforward options:

**Just use `web start` once:**
```bash
botson web start --port 8080
```
It stays up until `botson web stop` or a reboot. Every `botson tui` / `botson discord start` invocation from then on attaches to it instead of embedding its own.

**Or run it under a real service supervisor** (recommended for anything you want to survive a reboot). Since `runWeb` always registers daemon state regardless of how it's launched, a plain foreground invocation under systemd works with no special flags:
```ini
# /etc/systemd/system/botson.service
[Unit]
Description=Botson core
After=network.target

[Service]
ExecStart=/root/.botsonv2/bin/botson web --port 8080
WorkingDirectory=/path/to/your/project
Restart=on-failure
User=youruser

[Install]
WantedBy=multi-user.target
```
```bash
systemctl enable --now botson
botson web status   # confirms it's visible the same way `web start` would leave it
```
`WorkingDirectory` here plays the same role `os.Getwd()` does for a manually-launched `web start` — it's the workspace every tool call (readFile, runCommand, etc.) resolves against for that core's entire lifetime (see §7).

There's currently no bundled systemd unit file or install-time offer to set one up (Windows gets the tray-autostart equivalent via `setup install`; Linux/macOS don't have an analogous prompt yet) — see §8 for this as a concrete improvement idea.

---

## 7. Workspace directory resolution

Every tool the agent runs (`readFile`, `writeFile`, `runCommand`, etc.) resolves paths relative to *the core process's own working directory* — there's no per-session or per-request workspace concept. That directory is fixed for the entire lifetime of whichever process is acting as the core:

| How the core was started | Workspace used |
|---|---|
| `botson web` (foreground, manual) | Its own `os.Getwd()` at launch |
| `botson web start` | The *caller's* `os.Getwd()` at the moment `start` was run (explicitly threaded through `daemon.Start`'s `dir` parameter) |
| Systemd (or similar) | Whatever `WorkingDirectory=` (or equivalent) was configured |
| TUI's embedded core | Wherever the TUI itself was launched from — no extra step, since it's the same process, not a spawned child |
| Tray-launched web core | `config.AppConfig.WorkspaceDir` if set (since tray itself has no meaningful cwd), else tray's own cwd |

**Known limitation, not solved by any of this**: you cannot change an *already-running* core's workspace. It's pinned for that process's life — restart it from a different directory (or under a different systemd unit / different `--port` core entirely) to point it elsewhere. True per-session or per-tool-call workspace switching would mean threading a workspace argument through `agent.Context` and every tool built on `os.Getwd()` today — a materially bigger change than anything in this design so far.

---

## 8. Limitations and directions worth thinking about

These are open, not resolved — flagged here specifically because they seem like the more likely next places this design gets pushed on.

- **No supervision or auto-restart.** If the core crashes, nothing brings it back — a systemd unit with `Restart=on-failure` (§6) is currently the only way to get that, and it's not offered or documented anywhere except this file. Worth deciding whether Botson should ship an actual unit file / install-time offer for Linux, mirroring what `setup install` already does for the Windows tray's login autostart.
- **One workspace per core, for its whole life (§7).** If you want to work on two different projects with the agent at once, that's two entirely separate cores (different ports, no shared state, no shared model/session-service instance) rather than one core juggling both. Whether that's actually a limitation depends on how "multi-project" a workflow you expect — it's the single biggest architectural constraint of this design.
- **Discovery is single-host and unauthenticated.** Everything is `127.0.0.1`-only by convention, not by enforcement — there's no bind-address restriction, auth token, or TLS on the REST API. Fine for a local dev tool; would need real work before "point a client at a core running somewhere else" becomes a supported idea rather than something that happens to work if you're careless with `--port` and firewalls.
- **The TUI's embedded-core fallback re-pays the full bootstrap cost every time.** Loading the agent registry, opening the session DB, constructing the Gemini client — all of it happens fresh for every `botson tui` invocation that doesn't find a core to attach to. For a single quick session this is invisible; for someone who opens/closes the TUI frequently without ever running `web start`, it's wasted, repeated startup latency that a warm shared core would avoid entirely.
- **Two independent `botson tui` invocations with no shared core silently duplicate state** (§5.3) rather than erroring or warning that you're not actually getting a shared session. There's no nudge anywhere suggesting "you're about to open a second, disconnected instance — did you mean to `web start` first?"
- **Tray status is poll-based (3s), not pushed.** A toggle made from the web console or a `toggleDiscord` tool call takes up to 3 seconds to reflect in the tray's menu label. Minor, but a real, noticeable staleness window.
- **No log rotation.** `~/.botsonv2/logs/{web,tray}.log` grow forever for a long-lived core; nothing truncates or rotates them.
- **Windows and Unix asymmetry.** The loopback control-channel design exists specifically to work around Windows having no equivalent of sending a Unix signal to an arbitrary process — it's used uniformly on both platforms for consistency, but it does mean a slightly heavier mechanism (a real TCP round-trip) is used everywhere a plain `SIGTERM` would otherwise suffice on Linux/macOS.

---

## See also

- [AGENTS.md — "Unified core architecture"](../AGENTS.md) — the condensed version, plus how this connects to the HITL confirmation protocol, the import-direction rules, and the rest of the codebase's conventions.
- [docs/sessions.md](./sessions.md) — how session state/history is actually stored, independent of which process is serving it.
