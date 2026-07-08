# AGENTS.md

This file orients AI coding agents (and human maintainers doing a deep dive) working in this codebase. For a short, human-facing project overview, see [README.md](./README.md).

Botson is a Go-based agent framework built on Google's **ADK v2**, exposing the same agent/session/artifact backend through three interfaces (TUI, web console, Discord) from one primary binary, `botson`. As of 2026-07, these three interfaces share **one unified core process** rather than each independently bootstrapping their own copy of the Gemini model/agent registry/session state — see "Unified core architecture" below.

---

## Project structure

- **`/cmd`**: application entry points. `botson` is the primary one and the focus of ongoing design decisions; `botson-discord` and `botson-adk` are minimal standalone alternatives kept alongside it.
  - **[`/botson`](./cmd/botson/)**: the primary application (ships as `botsonv2-<os>-<arch>`) — a Cobra CLI with `tui` (default), `web`, `discord`, `tray` (Windows only), and `setup` subcommands. One binary boots a chat session, the custom web console + REST/A2A APIs, the Discord gateway, a system tray icon, or install/uninstall/reset itself. Also the testing ground for `core/interface/tui` and `core/interface/web`. Does not include the built-in ADK web launcher.
  - **[`/botson-discord`](./cmd/botson-discord/)**: standalone Discord Gateway bot listener — a minimal, single-interface deployment.
  - **[`/botson-adk`](./cmd/botson-adk/)**: only the standard, built-in Google ADK developer console/web launcher and APIs — useful for testing against ADK's own dev webserver, kept separate from `botson`.
- **`/core`**: main application packages.
  - **[`/agent`](./core/agent/)**: custom recursive agent loader, default definitions, and tool registry.
  - **[`/artifact`](./core/artifact/)**: local file system service for persistent artifacts.
  - **[`/config`](./core/config/)**: `AppConfig` struct, load/save/update, and workspace path lookups (`~/.botsonv2/`). `Load` caches a single shared instance per process and `Update` mutates it in place (see "Self-configuration" below) — this is the one package every settings-reading/writing code path (CLI, web API, agent tool) ultimately goes through, so it can't import `core/management`, `core/agent`, or `core/tools` without creating a cycle.
  - **[`/daemon`](./core/daemon/)**: generic detach/control lifecycle (start/stop/status, PID files, the loopback control channel) shared by every backgroundable subcommand (`web`, `tray`) — `discord` used to be one too, but is now an in-process toggle of a running `web` core instead (see "Unified core architecture" below).
  - **[`/setup`](./core/setup/)**: backs `botson setup install/uninstall/reset/status` — prompts (interactive and flag-driven), installing the binary to `~/.botsonv2/bin` and onto PATH, (Windows) tray-autostart registration, and a read-only status report.
  - **[`/interface`](./core/interface/)**: the three user-facing interfaces, plus the client package thin interfaces use to talk to the core.
    - **[`/web`](./core/interface/web/)**: serves the unified SPA console — embedded files, custom API handlers (`api_builder.go`, `api_dashboard.go`), sublauncher routing. `botson web` is the unified core process: this is where the REST/A2A APIs, the console, and (via `discord.InitCore`) the Discord gateway singleton all actually run.
    - **[`/discord`](./core/interface/discord/)**: Discord Gateway listener, command handlers (`commands.go`), security locks (`handlers.go`), DB/disk session persistence (`sessions.go`), HITL confirms (`hitl.go`), disk-persisted pending-authorization requests (`pending.go`), and the in-process gateway singleton (`singleton.go`) — see "Unified core architecture" below.
    - **[`/tui`](./core/interface/tui/)**: Bubble Tea terminal chat interface. A thin client of a running core (see below) — it holds an `*apiclient.Client`, not its own runner/agent loader.
    - **[`/apiclient`](./core/interface/apiclient/)**: minimal HTTP/SSE client over the core's REST API (`DefaultAgent`, `CreateSession`, `Run`, plus Discord toggle/status), used by the TUI and by `botson discord start/stop/status` so those don't need their own Gemini/agent bootstrap.
  - **[`/management`](./core/management/)**: shared, interface-agnostic business logic (agents, sessions, config, dashboard stats, Discord gateway control) callable from both the web API and the CLI, so `botson` and the webui always drive the exact same functions. `ListSessions`/`GetSession`/`DeleteSession` (`sessions.go`) only need a `session.Service`, not the full Gemini/agent-loader bootstrap `GetDashboardStats` needs — same reasoning as `ListAgents`. `discord_daemon.go` keeps its historical name and exported signatures (`StartDiscordDaemon`/`StopDiscordDaemon`/`DiscordDaemonStatus`) even though Discord is no longer a separate daemon process underneath — see "Unified core architecture" below.
  - **[`/session`](./core/session/)**: GORM & SQLite implementation for persisting conversation state. `InitPersistentSessionService` silences GORM's default logger (it writes to stdout, not stderr) at construction, since every consumer -- CLI JSON output, the TUI's alt-screen -- would otherwise get corrupted by it; don't reintroduce a per-consumer workaround for this (the TUI used to have one, an unsafe-reflection hack, removed once this was fixed at the source). See [docs/sessions.md](./docs/sessions.md) for the full schema/API reference.
  - **[`/tools`](./core/tools/)**: secure tools (`listFiles`, `readFile`, `writeFile`, `editFile`, `loadArtifacts`, `saveArtifact`, `updateSettings`, `runCommand`, `saveScript`, `runScript`, `toggleDiscord`). `readFile`/`writeFile`/`editFile` share path validation via `resolveWorkspacePath` (`workspace.go`) — the one place that confines a tool to the workspace root and blocks `.env` access, so fix path-safety bugs there rather than per-tool.
  - **[`/procutil`](./core/procutil/)**: `Run(ctx, name, args, opts)` — runs a subprocess with a timeout that actually works (kills the whole process group, not just the direct child) and truncates captured output. Leaf package (only depends on the stdlib), shared by `core/tools`' `runCommand` and `core/scripts`' script runner so this exec-safety logic exists in exactly one place.
  - **[`/scripts`](./core/scripts/)**: the named-script system — `List`/`Save`/`Delete`/`Run` over `~/.botsonv2/scripts/<name>/main.go` + a `script.json` sidecar for the description. Another leaf package (only depends on `core/config` and `core/procutil`), so both `cmd/botson` and `core/tools` (`saveScript`/`runScript`) can use it directly without an import cycle.

## Architecture / how it works

1. **Registry loading**: default agents (bundled) and custom user agents (from `~/.botsonv2/agents/`) are parsed and built recursively, supporting tool configuration and sub-agent delegation.
2. **Server hosting**: `botson web` runs the ADK web server — REST (`/api/*`), streaming (`/api/run_sse`), and the console SPA on `/botson/`. This is also the unified core process (see below): the same process holds the agent registry and can run the Discord gateway in-process.
3. **Discord gateway**: logs into Discord, registers slash commands (`/new`, `/list`, `/select`, `/info`, `/approve`), listens for incoming messages. Runs either inside the core (toggled via `toggleDiscord`/the web console/`botson discord start`) or as a fully standalone process (bare `botson discord`, no subcommand) — see below.
4. **Web console frontend**: single stylesheet/script architecture split into `main` (layout/view switching), `dashboard` (metrics, agent lists, session activity), `chat` (streaming chat, tool trace visualization, session inspection), `builder` (agent/prompt/tool config forms).

## Unified core architecture

See [docs/process-architecture.md](./docs/process-architecture.md) for the full deep dive (process inventory, discovery mechanics, lifecycle diagrams, and known limitations) — this section is the condensed version.

Historically, `botson tui`, `botson web`, and `botson discord` were three fully independent OS processes, each running its own copy of `setupApp()`'s bootstrap (Gemini model, agent registry, session service) with no in-memory sharing. This caused a real bug: a background process launched with no meaningful working directory of its own (e.g. the tray's login-time autostart) silently inherited whatever directory happened to be current at spawn time (`core/daemon.Start` never set the child's cwd). Fixing that properly, plus the desire to let the agent turn Discord on/off for the user without spawning a whole new process, motivated a bigger change (2026-07): **one core process holds the state; the other interfaces become thin clients of it.**

- **The core is `botson web`.** It already did the full bootstrap and already served a complete REST API (`/api/*`, ADK's own — session CRUD, `run_sse` streaming chat), so nothing new needed building there; it's just now the one process that matters. `cmd/botson/cmd_web.go` splits this into two functions: `runWeb` registers daemon state (`daemon.WriteState`, the loopback control channel) and then calls `runCoreServer`, which does the actual work (`discord.InitCore(boot.Launcher)`, launching ADK's REST/A2A/console sublaunchers). `runWeb` always registers, regardless of how the process was launched -- directly (`botson web`), detached (`web start`), or under an external supervisor like systemd (a plain `ExecStart=botson web` unit needs nothing special here; it doesn't need to self-detach). `web start`'s detached child is now just the plain `web` subcommand itself (`webDaemonChildArgs` builds `["web", "--port=N", ...]` with no separate hidden `__daemon-child` command anymore) -- the two used to differ only in *whether* they registered daemon state, and now they always do, so there's nothing left to distinguish. `runCoreServer` is called directly, bypassing registration, only by the TUI's private embedded core (see below) -- the one case that must stay undiscoverable.
- **`core/daemon`** (`daemon.Start(id, displayName, dir string, childArgs []string)`) now takes an explicit `dir` and threads it through to `child.Dir` — every spawn site (`web start`, tray) passes its own intentional directory instead of relying on ambient inheritance. `daemon.State`/`Status` also gained `Meta map[string]string`, used to stash the running core's actual REST API port (`Meta["apiPort"]`) so a client can find a non-default port. `config.AppConfig.WorkspaceDir` is the one exception to "callers pass their own cwd": the tray has no meaningful cwd of its own (launched via a login autostart entry), so it falls back to this field instead (set once by `setup install`, defaulting to wherever install was run from).
- **The TUI is a thin client, and prefers an already-running core -- but never silently starts one in the background.** `core/interface/apiclient.Client` wraps the core's REST API (`DefaultAgent`, `CreateSession`, `Run` — the last shaped like `iter.Seq2[*Event, error]`, deliberately mirroring `runner.Runner.Run` so `core/interface/tui/io.go`'s event loop needed minimal changes). `runTUI` (`cmd/botson/cmd_tui.go`) calls `ensureCoreRunning`, which checks `daemon.GetStatus("web", ...)`: if a real, discoverable core is already running (`botson web`, `web start`, or one under an external supervisor like systemd), it attaches to that over HTTP/SSE and never builds its own Gemini client, agent loader, or session service at all (hence `PersistentPreRunE: noBootstrap` on `tui`). **If no core is running, it does not spawn one as a detached background daemon** -- an earlier version of this did, which meant a bare `botson` (just opening the TUI) silently left a `web` process running in the background indefinitely, exactly the kind of surprise this whole redesign set out to avoid. Instead, `startEmbeddedCore` runs a full core inside the TUI's *own* process, on an ephemeral loopback port, doing its own `setupApp` bootstrap if needed and registering no daemon state at all -- nothing else can discover or stop it, and it disappears the instant the TUI exits, leaving nothing behind. A `--no-auto-start` flag fails outright instead of falling back to this embedded core, for anyone who wants to be certain they're always talking to an explicitly-started, shared core.
- **HITL in the TUI**: this thin-client rewrite also closed a pre-existing gap — the old TUI had no confirmation UI at all, so a `RequireConfirmation: true` tool call silently stalled forever. `core/interface/tui/io.go` now special-cases `FunctionCall.Name == "adk_request_confirmation"` (see "HITL confirmation wire protocol" below) and `tui.go`'s `Update()` gains `y`/`n` keybindings active only while a confirmation is pending.
- **Discord is an in-process, togglable singleton**, not a separate daemon. `core/interface/discord/singleton.go` holds a package-level `active *Gateway` behind a mutex (`InitCore`/`StartGateway`/`StopGateway`/`GatewayStatus`) — starting/stopping Discord is now just spinning a goroutine + discordgo session up or down within the core, no new OS process. `core/management/discord_daemon.go` keeps its historical exported names (`StartDiscordDaemon`/`StopDiscordDaemon`/`DiscordDaemonStatus`) so `core/interface/web/api_dashboard.go`'s `/discord/start|stop|status` handlers (and the web console's existing Start/Stop buttons) needed zero changes — only the implementation underneath swapped. The `toggleDiscord` agent tool (`core/tools/toggle_discord.go`, `RequireConfirmation: true`) calls `core/interface/discord` **directly**, not through `core/management` — routing through `management` would create an import cycle (`core/tools` → `core/management` → `core/agent` → `core/tools`), so this is the one place Discord control bypasses the `management` layer other callers use. See "Conventions" for the import-direction rule this follows.
- **`botson discord start/stop/status`** now retarget to whichever core is running, over HTTP (`core/interface/apiclient`'s Discord methods against `/botson/api/discord/*`), erroring clearly if no core is running rather than silently falling back to spawning a standalone process. Bare `botson discord` (no subcommand) is unchanged — still a genuinely standalone, foreground, core-independent process for anyone who wants Discord fully isolated (e.g. on a different machine).
- **The tray (Windows-only) and `setup status` follow the same rule.** `tray_windows.go`'s Discord menu item calls `discordCoreClient()` (shared with `cmd_discord.go`) instead of `daemon.Start/Stop` against a `"discord"` daemon id that no longer exists — this was actually a latent Windows build break introduced when Phase 3 removed that id, only caught by `GOOS=windows go build ./...` cross-compilation (no Windows machine is available to catch it any other way; there's no CI gate for this yet, so re-run that cross-compile after touching anything under `cmd/botson` with a `_windows.go` file). `core/setup/status.go`'s "Background services" report queries the running core's `/botson/api/discord/status` for Discord's row instead of a `.pid` file that no longer exists, falling back to "not running (core isn't running)" if there's no core to ask.
- **Known limitation, not solved by this**: switching an *already-running* core's workspace directory. It's pinned for that process's lifetime — restart it from a new directory to change it. True per-session/per-tool-call workspace switching would require threading a workspace argument through `agent.Context` and every tool built on `os.Getwd()`, a materially bigger change than this.

## Bare `botson` dispatch

A bare `botson` (no subcommand) runs whichever interface `config.AppConfig.DefaultCommand` names (`"tui"` / `"web"` / `"discord"`), via `runDefaultCommand` in `cmd/botson/main.go`. Empty or unrecognized values fall back to `"tui"`. This field is **not yet exposed** through `setup install` or any prompt — it's config.json-only for now, set by hand. When it resolves to `"tui"`, a bare `botson` on a fresh machine still works with no separate `web start` step first — it just runs its own private, in-process core rather than a shared one (see "Unified core architecture" above), so nothing is left running once you exit.

## Self-configuration

`core/config.Load()` returns a single cached `*AppConfig` per process (not a fresh read each call), and `core/config.Update(mutate func(*AppConfig))` edits that cached instance's fields **in place** before persisting to disk, rather than building a new struct and swapping the pointer. That means every long-lived holder of the config pointer within one process (`cmd/botson`'s `appBoot.Config`, anything else that called `Load()` earlier) sees an `Update` immediately, with no restart needed. `core/management.UpdateConfig` and `botson settings set` both go through `config.Update` for this reason — see `core/config/config_test.go` for the regression test guarding this specifically (it would be easy to "simplify" `Update` back into load-then-replace and silently break this).

This is what makes the `updateSettings` agent tool (`core/tools/update_settings.go`) meaningful: the running agent can change its own model/root-agent/default-command mid-conversation and have it actually take effect for the rest of that process's life, not just on next launch. It deliberately excludes secrets (Gemini API key, Discord token/owner) — those stay human-controlled via `botson settings set` or the web console, so a confused or compromised agent can't rotate or wipe its own credentials. `RequireConfirmation: true` is set on its registry entry (`core/agent/registry.go`), same as `saveArtifact`, so it still pauses for a HITL approval before taking effect.

## Coding/exec tools

`writeFile` and `runCommand` (`core/tools/write_file.go`, `core/tools/run_command.go`) give the agent real editing and shell-execution capability in the project workspace, on top of the earlier read-only `readFile`/`listFiles`. Both default to `RequireConfirmation: true` in the registry, same posture as `saveArtifact`/`updateSettings` — this was a deliberate choice (2026-07) since it's the biggest capability jump in the tool registry so far, not because the code backing them is untrusted.

`runCommand` runs the given string through the platform's own shell (`/bin/sh -c` / `cmd /C`) in the workspace root, via `core/procutil.Run` (timeout default 120s, output capped at ~200KB to protect the agent's own context). `procutil.Run` is the one place that handles two easy-to-get-wrong things correctly: killing the *whole process group* on timeout rather than just the direct child (`exec.CommandContext` alone would leave a forked-not-exec'd grandchild running, e.g. `sh -c "sleep 5"` on this box, holding the captured stdout/stderr pipe open past the shell's own death and silently defeating the timeout — see `core/procutil/procutil_test.go`'s timeout case), and correctly classifying "killed by our own timeout" separately from "process ran and exited non-zero" (a SIGKILL'd process surfaces as the same `*exec.ExitError` type as a normal non-zero exit, so the timeout check has to run first).

**`editFile`** (`core/tools/edit_file.go`, added 2026-07 mirroring how Claude Code's own Edit tool works) makes a precise find-and-replace edit — `oldString` must match the file's current content exactly, and exactly once unless `replaceAll` is set — rather than requiring `writeFile` to regenerate an entire file from memory just to change a few lines. `readFile` (`core/tools/read_file.go`) was rewritten alongside it to return `cat -n`-style line-numbered, paginated output (`offset`/`limit`, default 2000-line limit) instead of the whole file as one string, so a line number it reports can be quoted directly in a following `editFile` call.

**Read-before-write guard** (`core/tools/read_tracking.go`): `writeFile` and `editFile` both refuse to touch a file that hasn't been read via `readFile` earlier in the *same session* — except a brand-new file, which is exempt (nothing to have read). Tracking uses `agent.Context.State()`, verified to be a durable, session-scoped key/value store (traced through `agent/common_context.go` → the ADK runner's `StateDelta` → `session/database/service.go` → `core/session/persistent.go`'s GORM/SQLite backing). **Key-shape matters here**: one flat state key per absolute path (`"botson:tools:read:" + fullPath` → `bool`), not one key holding a `map[string]bool` — session state is JSON round-tripped on reload, so a map value comes back as `map[string]interface{}` on a later turn while a bool round-trips losslessly as a bool either way. The guard fails open (silently skipped) if `ctx`/`ctx.State()` is nil, which only happens in hand-written unit tests, never in production tool invocation — see `core/tools/fake_context_test.go`'s `fakeContext` (embeds `agent.ContextMock`, overrides `State()`) for the test double that lets tests actually exercise the guard instead of bypassing it.

Verified live end-to-end (not just unit-tested): drove a real conversation through `/api/run_sse` asking the agent to edit a file. It chose `listFiles` → `readFile` (confirmed the state delta really contained `"botson:tools:read:<path>": true`) → `editFile` with a whitespace-exact `oldString`/`newString` pulled straight from the numbered read → paused for HITL confirmation (since `editFile` is `RequireConfirmation: true`) → after approval, the file was changed correctly with nothing else disturbed, and the agent even re-read the file on its own to confirm.

## HITL confirmation wire protocol

ADK's `RequireConfirmation: true` (used by `saveArtifact`, `updateSettings`, `writeFile`, `editFile`, `runCommand`, `saveScript`, `runScript`, `toggleDiscord`) does **not** simply pause and resume the original tool call. Verified 2026-07 by driving a real `writeFile` call through `/api/run_sse` directly and inspecting the raw persisted session -- the actual sequence for one gated call is:

1. The model's real `functionCall` (e.g. `writeFile`, some call id `X`).
2. An **immediate** `functionResponse` for that same call id `X`, before any human has done anything: `{"response": {"error": "error tool \"writeFile\" requires confirmation, please approve or reject"}}`. This is ADK's own internal bookkeeping for "this call is now blocked pending confirmation" -- it is not a real result, even though it has exactly the shape of one.
3. A synthetic wrapper `functionCall` named `adk_request_confirmation` (a **new**, different call id), whose `args.originalFunctionCall` embeds the real call (name, id `X`, args) and whose `args.toolConfirmation.hint` is the prompt to show the user. This is what the frontend should actually render as "pending approval" (`appendHitlPending`).
4. The human's decision arrives as a `functionResponse` on the `adk_request_confirmation` call id, `{"response": {"confirmed": true|false}}`.
5. Only *then*, if approved, does the real tool handler run -- producing a **second** `functionResponse` reusing the *original* call id `X`, this time with the tool's actual result.

The trap: call id `X` gets two different `functionResponse`s over the call's lifetime (the fake "requires confirmation" placeholder, then the real result), and naively keying a "call id → its result" lookup off the last-seen `functionResponse` (as `core/interface/web/webui/static/js/chat.js`'s `selectSession` briefly did) works fine once everything's settled, but shows the tool call as falsely "Completed" with the placeholder error in the window between steps 2 and 5 -- i.e. exactly while the real `adk_request_confirmation` card is showing "pending". The fix: track which call ids appear as some `adk_request_confirmation`'s `args.originalFunctionCall.id`, and never render those ids' raw `functionCall` as their own trace at all -- their whole story (pending → approved/denied → result) belongs to the confirmation card alone. See `chat.js`'s `confirmationOriginalIds` set.

## Named scripts

`botson script`/the `saveScript`+`runScript` tools (`core/scripts/`) let the user or the agent save a small Go program under `~/.botsonv2/scripts/<name>/main.go` (+ a `script.json` sidecar just holding the description) and run it by name afterward, instead of a fixed one-off CLI subcommand or the agent re-writing equivalent code inline every time. `Run` **builds the script to a temp binary and executes that directly** rather than `go run <path>` — `go run` always reports exit code 1 on any non-zero child exit regardless of the actual code (it just prints "exit status N" to stderr), which would silently discard the real exit code callers rely on; building still benefits from `GOCACHE` so repeat runs stay fast. See `core/scripts/scripts_test.go`'s `TestRunPreservesExitCode` for the regression test — this was a real bug caught while building it, not a theoretical one. `saveScript`/`runScript` both default to `RequireConfirmation: true`, same posture as `runCommand`: writing new Go code that will later execute carries the same risk as running a shell command directly.

## Platform-specific files

Windows-only functionality (tray icon, autostart registration, uninstall self-delete helper, `procutil`'s process-group kill) is split via Go build tags into `_windows.go` / `_unix.go` / `_other.go` files (e.g. `tray_windows.go` vs `tray_other.go`, `autostart_windows.go` vs `autostart_unix.go`, `procutil_windows.go` vs `procutil_unix.go`). When adding a platform-specific feature, follow this pattern rather than runtime `if runtime.GOOS` branching inside shared files, except where the branch is small and genuinely one-off (e.g. the tray-specific prompts inside `core/setup/install.go`'s `Install`, or the shell/flag selection in `core/tools/run_command.go`).

The non-Windows `tray` command (`cmd/botson/tray_other.go`) is registered with `Hidden: true` so it doesn't clutter `botson help` where it can't do anything — it still runs and returns a clear "Windows only" error if invoked directly.

## CLI reference

Build platform binaries into `/bin`:
```bash
go run scripts/build_windows.go   # Windows
go run scripts/build_linux.go     # Linux
```

### First-run setup

```bash
botson setup install
```
Interactive wizard: Gemini API key, root agent (validated against `management.ListAgents()`, which needs no model/API key), optional Discord token/owner, then copies the binary to `~/.botsonv2/bin` and adds it to PATH. Re-running later detects an existing config and asks before overwriting, so it doubles as a repair/update step. On Windows it also offers to register/start the tray icon.

**Scripted / non-interactive install** (for agents or automated setup — added so this can be driven without a terminal attached for prompts):
```bash
botson setup install --non-interactive --gemini-api-key "KEY" [flags...]
```
| Flag | Notes |
|---|---|
| `--non-interactive` | required to activate flag-driven mode at all |
| `--gemini-api-key` | required on a first-ever install; keeps the existing key if omitted on a re-run |
| `--model` | default `gemini-3.1-flash-lite` |
| `--root-agent` | default `Agent Botson` |
| `--discord` | tri-state: `true` enables, `false` disables/clears, **omit** to leave existing Discord config untouched |
| `--discord-token` | required if `--discord=true` and no token is already saved |
| `--discord-owner-id` | optional |
| `--tray-autostart`, `--start-tray` | Windows only, no-op elsewhere |

Any flag left unset falls back to whatever's already in `config.json` (or a built-in default on a brand-new install) rather than prompting — see `core/setup/install.go`'s `InstallOptions`/`applyInstallOptions` for the exact precedence.

### Running

```bash
botson                          # same as `tui` — interactive terminal chat (unless DefaultCommand overrides this)
botson tui --agent "Some Agent" # thin client; attaches to a running core if there is one, else runs a private one in-process (nothing left running after exit)
botson web --port=8080          # the unified core: REST/A2A APIs, web console, and (via toggleDiscord) Discord
botson discord                  # standalone Discord gateway, foreground, tied to this terminal, independent of any core
botson --help                   # list all commands and flags
```

`web` runs as a detached background process with a PID-file-backed lifecycle:
```bash
botson web start --port=8080 / status / stop [--force]
```
Logs: `~/.botsonv2/logs/web.log`. State: `~/.botsonv2/web.pid`. Since Windows has no signal-based graceful shutdown for an arbitrary detached process, `stop` talks to a small loopback control channel the background process opens instead — this works identically on Linux.

`discord start/stop/status` no longer spawn a separate process — they call the running core's `/botson/api/discord/*` endpoints over HTTP (via `core/interface/apiclient`), erroring if no core is running:
```bash
botson discord start / status / stop
```

On Windows, `tray` mirrors and controls the web core via the same state files/logic (`tray`, `tray start/status/stop [--force]`) — closing the tray never stops the core, since it's just another client of the same daemon state. Its Discord menu item is a thin HTTP toggle against the core's `/botson/api/discord/*`, same as the CLI's `discord start/stop/status`.

### Setup lifecycle: uninstall / reset / status

```bash
botson setup uninstall                        # ask per step: PATH, startup, binary, keep config.json?
botson setup uninstall --force-full-uninstall # skip every prompt, completely wipe ~/.botsonv2
botson setup reset                            # interactive, per-category keep/replace
botson setup status                           # read-only report on install/PATH/autostart/daemon state
```
`uninstall` asks up to three yes/no questions (remove from PATH? remove Startup/tray-autostart on Windows? delete the installed binary?) so any one can be done alone. Deleting the binary is the "real" uninstall step — it stops any running `discord`/`web`/`tray` daemons first, then asks whether to keep `config.json` (deleting sessions/custom agents/logs either way). `--force-full-uninstall` skips all prompts and wipes everything including `config.json`.

`reset` asks per-category ("keep your Gemini API key?", "keep your Discord settings?") whether to keep or replace, reusing `install`'s own prompt functions, and separately (defaulting to *no*) whether to wipe session history and custom agents. Always ends with a valid, saved config.

`status` makes no changes — reports whether the Gemini key/Discord/root agent are configured, whether the binary is installed and on PATH, tray autostart registration, and whether `web`/`tray` are currently running plus (queried from a running core, if any) Discord's status.

### Settings

```bash
botson settings get [--json]
botson settings set [--json] --model X --root-agent Y --default-command tui|web|discord --discord-token TOK --discord-owner-id ID --gemini-api-key KEY
```
Thin CLI wrapper over `core/management`'s `GetMaskedConfig`/`UpdateConfig` (the same functions the web Settings tab uses). `get` prints a masked summary or, with `--json`, the same masked struct as JSON. `set` only touches the flags you actually pass (checked via `cmd.Flags().Changed(...)`, same pattern as `setup install --non-interactive`) — everything else keeps its current value. Both skip the full agent/model bootstrap (`PersistentPreRunE: noBootstrap`), same reasoning as `setup`: a broken or missing config is exactly the thing `settings set` needs to be usable to fix.

### Agents

```bash
botson agents list [--json]
botson agents show <name> [--json]
botson agents tools [--json]
botson agents create --name X [--description Y] [--tools a,b,c] [--instructions "..." | --instructions-file path] [--private] [--json]
botson agents delete <name>
```
Thin CLI wrapper over `core/management/agents.go`'s `ListAgents`/`SaveAgent`/`DeleteAgent`/`ListTools` — the same functions the web Builder tab already used, now with a CLI front door too. `create` always writes a full replacement (`config.json` + `instructions.md` under `~/.botsonv2/agents/<name>/`) rather than a partial patch — there's no existing per-field "only touch what I pass" merge for agents the way `settings set`/`setup install` have for config, so re-running `create` on an existing name overwrites its description/tools/instructions wholesale. `tools` lists the exact strings valid in `--tools` (the standard registry from `core/agent/registry.go` plus any other agent name, for sub-agent delegation). `delete` only affects custom user agents — bundled defaults have no user-directory counterpart to remove, and return `management.ErrAgentNotFound`. None of these need the Gemini/agent bootstrap (`PersistentPreRunE: noBootstrap`).

### Scripts

```bash
botson script list [--json]
botson script show <name> [--json]
botson script create --name X [--description Y] --source-file path.go [--json]
botson script delete <name>
botson script run <name> [--timeout N] [-- args...]
```
Thin CLI wrapper over `core/scripts` — see "Named scripts" above for what a script actually is and why `Run` builds-then-executes instead of `go run`. `run`'s flag parsing uses `SetInterspersed(false)` so anything after `<name>`, flag-shaped or not, passes straight through to the script rather than `botson` trying to parse it as its own flag; a leading `--` (the conventional kubectl/docker-exec-style separator) is stripped by hand since Cobra doesn't do that itself once interspersed parsing is off. `--timeout` (botson's own flag, must come *before* `<name>`) only bounds the script's own execution — the build step always gets a separate, generous flat timeout, since `timeoutSeconds` describes the program's logic, not how long compiling it takes.

### Sessions

```bash
botson sessions list [--agent NAME] [--user ID] [--json]
botson sessions show <session-id> --agent NAME --user ID [--json]
botson sessions delete <session-id> --agent NAME --user ID
```
Thin CLI wrapper over `core/management`'s `ListSessions`/`GetSession`/`DeleteSession`, built directly on `core/session.InitPersistentSessionService` + `management.ListAgents()` rather than the full Gemini/agent-loader bootstrap — so, like `settings`/`agents`/`scripts`, it works even without a configured API key. A session's true identity is the composite key `(AppName, UserID, SessionID)` (see [docs/sessions.md](./docs/sessions.md)), not just the ID alone, which is why `show`/`delete` require `--agent`/`--user` — get those from `list`'s output first. `list`'s `eventCount` is always `0`: the underlying ADK `List` call doesn't preload events (only `Get` does, which `show` uses) — a pre-existing characteristic of the library, not a bug specific to this CLI, and the same limitation `core/management.GetDashboardStats`'s web dashboard stats already have.

### Standalone binaries

```bash
go run cmd/botson-discord/main.go   # Discord integration only
go run cmd/botson-adk/main.go       # stock ADK dev console/APIs only
```

## Configuration reference

`~/.botsonv2/config.json`:
```json
{
  "model_name": "gemini-3.1-flash-lite",
  "gemini_api_key": "your_api_key_here",
  "root_agent": "Agent Botson",
  "discord": {
    "token": "your_discord_token_here",
    "owner_id": "your_discord_owner_user_id",
    "whitelist": []
  },
  "default_command": "",
  "workspace_dir": ""
}
```
- No `discord.enabled` field, deliberately — whether the gateway runs is controlled entirely by `toggleDiscord`/`discord start`/`stop`/the webui's Start/Stop button, which all drive the same in-process singleton (see "Unified core architecture" above).
- `default_command` (`""` / `"tui"` / `"web"` / `"discord"`) picks what a bare `botson` runs; see "Bare `botson` dispatch" above. Settable via `botson settings set --default-command` or the `updateSettings` agent tool; not yet exposed via `setup install`.
- `workspace_dir` is only consulted by processes with no meaningful working directory of their own (the tray, launched via login autostart) — everything launched from a terminal (`web start`, `discord start`) uses its own actual cwd instead and ignores this field. The TUI's embedded core (see "Unified core architecture") doesn't consult it either -- being in-process, it simply runs in whatever directory the TUI itself was started from, with nothing to configure. Set once by `setup install` (defaulting to wherever install itself was run from); omitted (`""`) means "fall back to that process's own cwd."
- Read/write this file through `botson settings get/set`, the web Settings tab, or the `updateSettings` tool rather than hand-editing while a `botson` process is running, so the in-memory copy that process is holding doesn't drift from disk — see "Self-configuration" above.

## Dependencies

Prefer the standard library where it can reasonably do the job; the project leans on these specific third-party packages rather than pulling in new ones casually:

- `google.golang.org/adk/v2` — core Agent Development Kit
- `google.golang.org/genai` — Gemini API client
- `github.com/gorilla/mux` — router for custom API routing
- `github.com/bwmarrin/discordgo` — Discord API bindings
- `github.com/spf13/cobra` — CLI command/flag framework powering `botson`
- `github.com/getlantern/systray` — cross-platform system tray icon (Windows `tray` subcommand)
- `golang.org/x/term` — masked (password-style) terminal input for `setup install`/`reset` prompts
- `gorm.io/gorm` (+ `glebarez/sqlite`) — ORM/SQLite backing session persistence
- `github.com/charmbracelet/{bubbletea,bubbles,lipgloss}` — the TUI

## Conventions

- Commit messages follow Conventional Commits style: `feat:`, `fix:`, `refactor:`, etc., imperative mood, no trailing period.
- Prefer adding a flag with a sensible default over introducing a new prompt, when a feature needs to be scriptable (see `--non-interactive` on `setup install`, `--force-full-uninstall`).
- Cobra commands that only manage a background process's lifecycle (not the agent runtime) set `PersistentPreRunE: noBootstrap` to skip the expensive config/Gemini/agent/session bootstrap — see `newDiscordStartCmd`, `newWebStopCmd`, etc.
- Import direction: `cmd/botson` → `core/management` → `core/agent` → `core/tools` → `core/config`. `core/tools` must never import `core/management` or `core/agent` (it would cycle back through `core/agent`'s import of `core/tools`) — shared logic those layers both need (e.g. `Mask`) belongs in `core/config` instead, not `core/management`.
