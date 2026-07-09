# AGENTS.md

This file orients AI coding agents (and human maintainers doing a deep dive) working in this codebase. For a short, human-facing project overview, see [README.md](./README.md).

Botson is a Go-based agent framework built on Google's **ADK v2**. As of 2026-07, it's one **core process** (the Gemini model, agent registry, session/artifact services) exposed over an embedded **NATS server** as the one true API any process talks to. This repo ships that core plus one interface built on it, the TUI (`botson`/`botson tui`) — see "Unified core architecture" below for why the web console and Discord gateway that used to live in this repo are gone, and what replaces them.

---

## Project structure

- **`/cmd`**: application entry points. `botson` is the only one — the focus of ongoing design decisions.
  - **[`/botson`](./cmd/botson/)**: the primary application (ships as `botsonv2-<os>-<arch>`) — a Cobra CLI with `tui` (default), `core`, `tray` (Windows only), and `setup` subcommands. One binary boots a chat session, the shared NATS-speaking core, a system tray icon, or install/uninstall/reset itself.
- **`/internal`**: main application packages.
  - **[`/agent`](./internal/agent/)**: custom recursive agent loader, default definitions, and tool registry.
  - **[`/artifact`](./internal/artifact/)**: local file system service for persistent artifacts.
  - **[`/config`](./internal/config/)**: `AppConfig` struct, load/save/update, and workspace path lookups (`~/.botsonv2/`). `Load` caches a single shared instance per process and `Update` mutates it in place (see "Self-configuration" below) — this is the one package every settings-reading/writing code path (CLI, agent tool) ultimately goes through, so it can't import `internal/management`, `internal/agent`, or `internal/tools` without creating a cycle.
  - **[`/daemon`](./internal/daemon/)**: generic detach/control lifecycle (start/stop/status, PID files, the loopback control channel) shared by every backgroundable subcommand (`core`, `tray`).
  - **[`/setup`](./internal/setup/)**: backs `botson setup install/uninstall/reset/status` — prompts (interactive and flag-driven), installing the binary to `~/.botsonv2/bin` and onto PATH, (Windows) tray-autostart registration, and a read-only status report.
  - **[`/interface`](./internal/interface/)**: the TUI, plus the packages either side of the core's NATS API.
    - **[`/natscore`](./internal/interface/natscore/)**: the server side of the core's NATS API. Subscribes to a handful of subjects (`botson.agent.default`, `botson.session.create`, `botson.session.get`, `botson.run`) and answers them using the same in-process `runner.Runner`/`session.Service`/`agent.Loader` calls any Go code in this process could make directly — see "Unified core architecture" below.
    - **[`/apiclient`](./internal/interface/apiclient/)**: the client side. Same package name and exported API (`DefaultAgent`, `CreateSession`, `GetSession`, `Run`) as when this was an HTTP/SSE client, but now a NATS client underneath — used by the TUI so it doesn't need its own Gemini/agent bootstrap.
    - **[`/tui`](./internal/interface/tui/)**: Bubble Tea terminal chat interface. A thin client of a running core (see below) — it holds an `*apiclient.Client`, not its own runner/agent loader.
  - **[`/management`](./internal/management/)**: shared, interface-agnostic business logic (agents, sessions, config) callable from the CLI, so every entry point drives the exact same functions. `ListSessions`/`GetSession`/`DeleteSession` (`sessions.go`) only need a `session.Service`, not the full Gemini/agent-loader bootstrap.
  - **[`/session`](./internal/session/)**: GORM & SQLite implementation for persisting conversation state. `InitPersistentSessionService` silences GORM's default logger (it writes to stdout, not stderr) at construction, since every consumer -- CLI JSON output, the TUI's alt-screen -- would otherwise get corrupted by it; don't reintroduce a per-consumer workaround for this (the TUI used to have one, an unsafe-reflection hack, removed once this was fixed at the source). See [docs/sessions.md](./docs/sessions.md) for the full schema/API reference.
  - **[`/tools`](./internal/tools/)**: secure tools (`listFiles`, `readFile`, `writeFile`, `editFile`, `loadArtifacts`, `saveArtifact`, `updateSettings`, `runCommand`, `saveScript`, `runScript`). `readFile`/`writeFile`/`editFile` share path validation via `resolveWorkspacePath` (`workspace.go`) — the one place that confines a tool to the workspace root and blocks `.env` access, so fix path-safety bugs there rather than per-tool.
  - **[`/procutil`](./internal/procutil/)**: `Run(ctx, name, args, opts)` — runs a subprocess with a timeout that actually works (kills the whole process group, not just the direct child) and truncates captured output. Leaf package (only depends on the stdlib), shared by `internal/tools`' `runCommand` and `internal/scripts`' script runner so this exec-safety logic exists in exactly one place.
  - **[`/scripts`](./internal/scripts/)**: the named-script system — `List`/`Save`/`Delete`/`Run` over `~/.botsonv2/scripts/<name>/main.go` + a `script.json` sidecar for the description. Another leaf package (only depends on `internal/config` and `internal/procutil`), so both `cmd/botson` and `internal/tools` (`saveScript`/`runScript`) can use it directly without an import cycle.

## Architecture / how it works

1. **Registry loading**: default agents (bundled) and custom user agents (from `~/.botsonv2/agents/`) are parsed and built recursively, supporting tool configuration and sub-agent delegation.
2. **Core hosting**: `botson core` runs an embedded NATS server (`nats-server/v2`, in-process, no external dependency) plus `internal/interface/natscore`'s subject handlers. This is the one process that holds the agent registry, session service, and artifact service in memory.
3. **The TUI**: a thin NATS client (`internal/interface/apiclient`) of a running core — see "Unified core architecture" below for how it finds (or privately becomes) one.

## Unified core architecture

See [docs/process-architecture.md](./docs/process-architecture.md) for the full deep dive (process inventory, discovery mechanics, lifecycle diagrams, and known limitations) — this section is the condensed version.

Historically, `botson tui`, `botson web`, and `botson discord` were three fully independent OS processes, each running its own copy of `setupApp()`'s bootstrap (Gemini model, agent registry, session service) with no in-memory sharing. A 2026-07 redesign fixed that: **one core process holds the state; the other interfaces become thin clients of it**, originally talking to it over HTTP (ADK's own REST/A2A launcher stack, plus a custom web console).

A second, bigger pivot (also 2026-07) replaced that HTTP surface with **NATS**: the core now exposes exactly one API — a handful of NATS subjects (`internal/interface/natscore`) — and that's the one true way any process talks to it, not an ADK/HTTP-specific mechanism. The motivation: make it trivial to build fully independent consuming microservices (a Discord bot, a web console) as *separate projects* that only need a NATS client and knowledge of the subjects/wire types in `internal/interface/natscore`, never an import of this module's internal packages. The web console and Discord gateway that used to live in this repo were removed as part of this pivot — they're expected to be rebuilt later as standalone projects against this same NATS API. This repo now ships the core plus exactly one interface built on it: the TUI.

- **The core is `botson core`** (`cmd/botson/cmd_core.go`, this pivot's replacement for the old `botson web`). `runCore` registers daemon state (`daemon.WriteState`, the loopback control channel) and calls `runCoreServer`, which does the actual work: start an embedded `*server.Server` (`nats-server/v2/server`) on a loopback port, wait for it to be ready, connect a `*nats.Conn` to it, and call `natscore.Serve(ctx, nc, boot.Launcher)` — which subscribes to every subject and blocks until ctx is cancelled. `runCore` always registers, regardless of how the process was launched — directly (`botson core`), detached (`core start`), or under an external supervisor like systemd. `runCoreServer` is called directly, bypassing registration, only by the TUI's private embedded core (see below) -- the one case that must stay undiscoverable.
- **`internal/daemon`** (`daemon.Start(id, displayName, dir string, childArgs []string)`) takes an explicit `dir` and threads it through to `child.Dir` — every spawn site (`core start`, tray) passes its own intentional directory instead of relying on ambient inheritance. `daemon.State`/`Status` also has `Meta map[string]string`, used to stash the running core's actual NATS port (`Meta["natsPort"]`) so a client can find a non-default port. `config.AppConfig.WorkspaceDir` is the one exception to "callers pass their own cwd": the tray has no meaningful cwd of its own (launched via a login autostart entry), so it falls back to this field instead (set once by `setup install`, defaulting to wherever install was run from).
- **The TUI is a thin client, and prefers an already-running core -- but never silently starts one in the background.** `internal/interface/apiclient.Client` wraps the core's NATS API (`DefaultAgent`, `CreateSession`, `GetSession`, `Run` — the last shaped like `iter.Seq2[*Event, error]`, deliberately mirroring `runner.Runner.Run` so `internal/interface/tui/io.go`'s event loop needed minimal changes across both the HTTP-to-NATS pivot and the original HTTP redesign before it). `runTUI` (`cmd/botson/cmd_tui.go`) calls `ensureCoreRunning`, which checks `daemon.GetStatus("core", ...)`: if a real, discoverable core is already running (`botson core`, `core start`, or one under an external supervisor like systemd), it connects to that over NATS and never builds its own Gemini client, agent loader, or session service at all (hence `PersistentPreRunE: noBootstrap` on `tui`). **If no core is running, it does not spawn one as a detached background daemon** — instead, `startEmbeddedCore` runs a full core (embedded NATS server + `natscore.Serve`) inside the TUI's *own* process, on an ephemeral loopback port, doing its own `setupApp` bootstrap if needed and registering no daemon state at all — nothing else can discover or stop it, and it disappears the instant the TUI exits, leaving nothing behind. A `--no-auto-start` flag fails outright instead of falling back to this embedded core, for anyone who wants to be certain they're always talking to an explicitly-started, shared core.
- **HITL in the TUI**: `internal/interface/tui/io.go` special-cases `FunctionCall.Name == "adk_request_confirmation"` (see "HITL confirmation wire protocol" below) and `tui.go`'s `Update()` gains `y`/`n` keybindings active only while a confirmation is pending. This protocol is unchanged by the NATS pivot -- it's ADK's own wire format for a gated tool call, carried as `genai.Content`/`FunctionCall`/`FunctionResponse` inside a `natscore.Event`/`Frame` instead of an SSE line, but the shape a caller reads and replies with is identical.
- **`botson.run`'s streaming protocol**: a single request/reply call can't carry the many events one agent turn produces, so `botson.run` is answered differently than the other three subjects. The client (`apiclient.Client.Run`, `internal/interface/apiclient/stream.go`) generates its own NATS inbox, subscribes to it, and publishes the request with that inbox as the reply subject; the server (`natscore.handleRun`) publishes one `natscore.Frame{Event: ...}` per `session.Event` the runner yields, terminated by exactly one final `Frame{Done: true}` or `Frame{Error: ...}` — there's no NATS-level "stream ended" signal the way an HTTP response body ending is implicit, so `Done`/`Error` are explicit. A manual publish-with-reply doesn't get nats.go's automatic "no responders" translation the way `nc.Request` does, so the client also checks the first frame's `Status` header for `"503"` (the raw NATS protocol signal for "nothing is subscribed to this subject") and translates that into the same "failed to reach core" error the other three calls get for free.
- **Known limitation, not solved by this**: switching an *already-running* core's workspace directory. It's pinned for that process's lifetime — restart it from a new directory to change it. True per-session/per-tool-call workspace switching would require threading a workspace argument through `agent.Context` and every tool built on `os.Getwd()`, a materially bigger change than this.

## Bare `botson` dispatch

A bare `botson` (no subcommand) always runs the TUI (`cmd/botson/main.go`'s `rootCmd.RunE` calls `runTUI` directly) — there's no other interface left in this binary to dispatch to. It works with no separate `core start` step first: it just runs its own private, in-process core rather than a shared one (see "Unified core architecture" above), so nothing is left running once you exit.

## Self-configuration

`internal/config.Load()` returns a single cached `*AppConfig` per process (not a fresh read each call), and `internal/config.Update(mutate func(*AppConfig))` edits that cached instance's fields **in place** before persisting to disk, rather than building a new struct and swapping the pointer. That means every long-lived holder of the config pointer within one process (`cmd/botson`'s `appBoot.Config`, anything else that called `Load()` earlier) sees an `Update` immediately, with no restart needed. `botson settings set` goes through `config.Update` for this reason — see `internal/config/config_test.go` for the regression test guarding this specifically (it would be easy to "simplify" `Update` back into load-then-replace and silently break this).

This is what makes the `updateSettings` agent tool (`internal/tools/update_settings.go`) meaningful: the running agent can change its own model/root-agent mid-conversation and have it actually take effect for the rest of that process's life, not just on next launch. It deliberately excludes secrets (the Gemini API key) — that stays human-controlled via `botson settings set`, so a confused or compromised agent can't rotate or wipe its own credentials. `RequireConfirmation: true` is set on its registry entry (`internal/agent/registry.go`), same as `saveArtifact`, so it still pauses for a HITL approval before taking effect.

## Coding/exec tools

`writeFile` and `runCommand` (`internal/tools/write_file.go`, `internal/tools/run_command.go`) give the agent real editing and shell-execution capability in the project workspace, on top of the earlier read-only `readFile`/`listFiles`. Both default to `RequireConfirmation: true` in the registry, same posture as `saveArtifact`/`updateSettings` — this was a deliberate choice (2026-07) since it's the biggest capability jump in the tool registry so far, not because the code backing them is untrusted.

`runCommand` runs the given string through the platform's own shell (`/bin/sh -c` / `cmd /C`) in the workspace root, via `internal/procutil.Run` (timeout default 120s, output capped at ~200KB to protect the agent's own context). `procutil.Run` is the one place that handles two easy-to-get-wrong things correctly: killing the *whole process group* on timeout rather than just the direct child (`exec.CommandContext` alone would leave a forked-not-exec'd grandchild running, e.g. `sh -c "sleep 5"` on this box, holding the captured stdout/stderr pipe open past the shell's own death and silently defeating the timeout — see `internal/procutil/procutil_test.go`'s timeout case), and correctly classifying "killed by our own timeout" separately from "process ran and exited non-zero" (a SIGKILL'd process surfaces as the same `*exec.ExitError` type as a normal non-zero exit, so the timeout check has to run first).

**`editFile`** (`internal/tools/edit_file.go`, added 2026-07 mirroring how Claude Code's own Edit tool works) makes a precise find-and-replace edit — `oldString` must match the file's current content exactly, and exactly once unless `replaceAll` is set — rather than requiring `writeFile` to regenerate an entire file from memory just to change a few lines. `readFile` (`internal/tools/read_file.go`) was rewritten alongside it to return `cat -n`-style line-numbered, paginated output (`offset`/`limit`, default 2000-line limit) instead of the whole file as one string, so a line number it reports can be quoted directly in a following `editFile` call.

**Read-before-write guard** (`internal/tools/read_tracking.go`): `writeFile` and `editFile` both refuse to touch a file that hasn't been read via `readFile` earlier in the *same session* — except a brand-new file, which is exempt (nothing to have read). Tracking uses `agent.Context.State()`, verified to be a durable, session-scoped key/value store (traced through `agent/common_context.go` → the ADK runner's `StateDelta` → `session/database/service.go` → `internal/session/persistent.go`'s GORM/SQLite backing). **Key-shape matters here**: one flat state key per absolute path (`"botson:tools:read:" + fullPath` → `bool`), not one key holding a `map[string]bool` — session state is JSON round-tripped on reload, so a map value comes back as `map[string]interface{}` on a later turn while a bool round-trips losslessly as a bool either way. The guard fails open (silently skipped) if `ctx`/`ctx.State()` is nil, which only happens in hand-written unit tests, never in production tool invocation — see `internal/tools/fake_context_test.go`'s `fakeContext` (embeds `agent.ContextMock`, overrides `State()`) for the test double that lets tests actually exercise the guard instead of bypassing it.

Verified live end-to-end (not just unit-tested): drove a real conversation through the core asking the agent to edit a file. It chose `listFiles` → `readFile` (confirmed the state delta really contained `"botson:tools:read:<path>": true`) → `editFile` with a whitespace-exact `oldString`/`newString` pulled straight from the numbered read → paused for HITL confirmation (since `editFile` is `RequireConfirmation: true`) → after approval, the file was changed correctly with nothing else disturbed, and the agent even re-read the file on its own to confirm.

## HITL confirmation wire protocol

ADK's `RequireConfirmation: true` (used by `saveArtifact`, `updateSettings`, `writeFile`, `editFile`, `runCommand`, `saveScript`, `runScript`) does **not** simply pause and resume the original tool call. Verified 2026-07 by driving a real `writeFile` call through the core directly and inspecting the raw persisted session -- the actual sequence for one gated call is:

1. The model's real `functionCall` (e.g. `writeFile`, some call id `X`).
2. An **immediate** `functionResponse` for that same call id `X`, before any human has done anything: `{"response": {"error": "error tool \"writeFile\" requires confirmation, please approve or reject"}}`. This is ADK's own internal bookkeeping for "this call is now blocked pending confirmation" -- it is not a real result, even though it has exactly the shape of one.
3. A synthetic wrapper `functionCall` named `adk_request_confirmation` (a **new**, different call id), whose `args.originalFunctionCall` embeds the real call (name, id `X`, args) and whose `args.toolConfirmation.hint` is the prompt to show the user. This is what a caller should actually render as "pending approval".
4. The human's decision arrives as a `functionResponse` on the `adk_request_confirmation` call id, `{"response": {"confirmed": true|false}}`.
5. Only *then*, if approved, does the real tool handler run -- producing a **second** `functionResponse` reusing the *original* call id `X`, this time with the tool's actual result.

The trap: call id `X` gets two different `functionResponse`s over the call's lifetime (the fake "requires confirmation" placeholder, then the real result), and naively keying a "call id → its result" lookup off the last-seen `functionResponse` works fine once everything's settled, but shows the tool call as falsely "Completed" with the placeholder error in the window between steps 2 and 5 -- i.e. exactly while the real `adk_request_confirmation` card is showing "pending". The fix (see `internal/interface/tui/replay.go`): track which call ids appear as some `adk_request_confirmation`'s `args.originalFunctionCall.id`, and never render those ids' raw `functionCall` as their own trace at all -- their whole story (pending → approved/denied → result) belongs to the confirmation card alone.

## Named scripts

`botson script`/the `saveScript`+`runScript` tools (`internal/scripts/`) let the user or the agent save a small Go program under `~/.botsonv2/scripts/<name>/main.go` (+ a `script.json` sidecar just holding the description) and run it by name afterward, instead of a fixed one-off CLI subcommand or the agent re-writing equivalent code inline every time. `Run` **builds the script to a temp binary and executes that directly** rather than `go run <path>` — `go run` always reports exit code 1 on any non-zero child exit regardless of the actual code (it just prints "exit status N" to stderr), which would silently discard the real exit code callers rely on; building still benefits from `GOCACHE` so repeat runs stay fast. See `internal/scripts/scripts_test.go`'s `TestRunPreservesExitCode` for the regression test — this was a real bug caught while building it, not a theoretical one. `saveScript`/`runScript` both default to `RequireConfirmation: true`, same posture as `runCommand`: writing new Go code that will later execute carries the same risk as running a shell command directly.

## Platform-specific files

Windows-only functionality (tray icon, autostart registration, uninstall self-delete helper, `procutil`'s process-group kill) is split via Go build tags into `_windows.go` / `_unix.go` / `_other.go` files (e.g. `tray_windows.go` vs `tray_other.go`, `autostart_windows.go` vs `autostart_unix.go`, `procutil_windows.go` vs `procutil_unix.go`). When adding a platform-specific feature, follow this pattern rather than runtime `if runtime.GOOS` branching inside shared files, except where the branch is small and genuinely one-off (e.g. the tray-specific prompts inside `internal/setup/install.go`'s `Install`, or the shell/flag selection in `internal/tools/run_command.go`).

The non-Windows `tray` command (`cmd/botson/tray_other.go`) is registered with `Hidden: true` so it doesn't clutter `botson help` where it can't do anything — it still runs and returns a clear "Windows only" error if invoked directly.

No Windows machine is available in this environment to actually run `tray_windows.go` — changes to it are verified with `GOOS=windows GOARCH=amd64 go build ./cmd/botson/...` cross-compilation only. Re-run that after touching anything under `cmd/botson` with a `_windows.go` file; there's no CI gate for this yet.

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
Interactive wizard: Gemini API key, root agent (validated against `management.ListAgents()`, which needs no model/API key), then copies the binary to `~/.botsonv2/bin` and adds it to PATH. Re-running later detects an existing config and asks before overwriting, so it doubles as a repair/update step. On Windows it also offers to register/start the tray icon.

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
| `--tray-autostart`, `--start-tray` | Windows only, no-op elsewhere |

Any flag left unset falls back to whatever's already in `config.json` (or a built-in default on a brand-new install) rather than prompting — see `internal/setup/install.go`'s `InstallOptions`/`applyInstallOptions` for the exact precedence.

### Running

```bash
botson                          # same as `tui` — interactive terminal chat
botson tui --agent "Some Agent" # thin client; attaches to a running core if there is one, else runs a private one in-process (nothing left running after exit)
botson tui --session ID         # reattach to an existing session instead of starting a new one, replaying its prior turns into the transcript
botson tui --session ID --user web --agent "Some Agent" # resume a session created under a different user ID -- `botson sessions list` shows its actual userId/agent
botson core --port=4222         # the shared core: an embedded NATS server plus internal/interface/natscore's subject handlers
botson --help                   # list all commands and flags
```

`core` runs as a detached background process with a PID-file-backed lifecycle:
```bash
botson core start --port=4222 / status / stop [--force]
```
Logs: `~/.botsonv2/logs/core.log`. State: `~/.botsonv2/core.pid`. Since Windows has no signal-based graceful shutdown for an arbitrary detached process, `stop` talks to a small loopback control channel the background process opens instead — this works identically on Linux.

On Windows, `tray` mirrors and controls the core via the same state files/logic (`tray`, `tray start/status/stop [--force]`) — closing the tray never stops the core, since it's just another client of the same daemon state.

### Setup lifecycle: uninstall / reset / status

```bash
botson setup uninstall                        # ask per step: PATH, startup, binary, keep config.json?
botson setup uninstall --force-full-uninstall # skip every prompt, completely wipe ~/.botsonv2
botson setup reset                            # interactive, per-category keep/replace
botson setup status                           # read-only report on install/PATH/autostart/daemon state
```
`uninstall` asks up to three yes/no questions (remove from PATH? remove Startup/tray-autostart on Windows? delete the installed binary?) so any one can be done alone. Deleting the binary is the "real" uninstall step — it stops any running `core`/`tray` daemons first, then asks whether to keep `config.json` (deleting sessions/custom agents/logs either way). `--force-full-uninstall` skips all prompts and wipes everything including `config.json`.

`reset` asks per-category ("keep your Gemini API key?", "keep your root agent selection?") whether to keep or replace, reusing `install`'s own prompt functions, and separately (defaulting to *no*) whether to wipe session history and custom agents. Always ends with a valid, saved config.

`status` makes no changes — reports whether the Gemini key/root agent are configured, whether the binary is installed and on PATH, tray autostart registration, and whether `core`/`tray` are currently running.

### Settings

```bash
botson settings get [--json]
botson settings set [--json] --model X --root-agent Y --gemini-api-key KEY
```
Thin CLI wrapper over `internal/management`'s `GetMaskedConfig`. `get` prints a masked summary or, with `--json`, the same masked struct as JSON. `set` only touches the flags you actually pass (checked via `cmd.Flags().Changed(...)`, same pattern as `setup install --non-interactive`) — everything else keeps its current value. Both skip the full agent/model bootstrap (`PersistentPreRunE: noBootstrap`), same reasoning as `setup`: a broken or missing config is exactly the thing `settings set` needs to be usable to fix.

### Agents

```bash
botson agents list [--json]
botson agents show <name> [--json]
botson agents tools [--json]
botson agents create --name X [--description Y] [--tools a,b,c] [--instructions "..." | --instructions-file path] [--private] [--json]
botson agents delete <name>
```
Thin CLI wrapper over `internal/management/agents.go`'s `ListAgents`/`SaveAgent`/`DeleteAgent`/`ListTools`. `create` always writes a full replacement (`config.json` + `instructions.md` under `~/.botsonv2/agents/<name>/`) rather than a partial patch — there's no existing per-field "only touch what I pass" merge for agents the way `settings set`/`setup install` have for config, so re-running `create` on an existing name overwrites its description/tools/instructions wholesale. `tools` lists the exact strings valid in `--tools` (the standard registry from `internal/agent/registry.go` plus any other agent name, for sub-agent delegation). `delete` only affects custom user agents — bundled defaults have no user-directory counterpart to remove, and return `management.ErrAgentNotFound`. None of these need the Gemini/agent bootstrap (`PersistentPreRunE: noBootstrap`).

### Scripts

```bash
botson script list [--json]
botson script show <name> [--json]
botson script create --name X [--description Y] --source-file path.go [--json]
botson script delete <name>
botson script run <name> [--timeout N] [-- args...]
```
Thin CLI wrapper over `internal/scripts` — see "Named scripts" above for what a script actually is and why `Run` builds-then-executes instead of `go run`. `run`'s flag parsing uses `SetInterspersed(false)` so anything after `<name>`, flag-shaped or not, passes straight through to the script rather than `botson` trying to parse it as its own flag; a leading `--` (the conventional kubectl/docker-exec-style separator) is stripped by hand since Cobra doesn't do that itself once interspersed parsing is off. `--timeout` (botson's own flag, must come *before* `<name>`) only bounds the script's own execution — the build step always gets a separate, generous flat timeout, since `timeoutSeconds` describes the program's logic, not how long compiling it takes.

### Sessions

```bash
botson sessions list [--agent NAME] [--user ID] [--json]
botson sessions show <session-id> --agent NAME --user ID [--json]
botson sessions delete <session-id> --agent NAME --user ID
```
Thin CLI wrapper over `internal/management`'s `ListSessions`/`GetSession`/`DeleteSession`, built directly on `internal/session.InitPersistentSessionService` + `management.ListAgents()` rather than the full Gemini/agent-loader bootstrap — so, like `settings`/`agents`/`scripts`, it works even without a configured API key, and even without a core running at all (it opens the session database file directly, not through NATS). A session's true identity is the composite key `(AppName, UserID, SessionID)` (see [docs/sessions.md](./docs/sessions.md)), not just the ID alone, which is why `show`/`delete` require `--agent`/`--user` — get those from `list`'s output first. `list`'s `eventCount` is always `0`: the underlying ADK `List` call doesn't preload events (only `Get` does, which `show` uses) — a pre-existing characteristic of the library, not a bug specific to this CLI.

`sessions show`/`delete` are read-only/lifecycle only -- they can't continue a conversation. To actually resume chatting in a past session, use `botson tui --session ID` (`--agent`/`--user` too, if it's not the default agent or wasn't created by the TUI): unlike `sessions show`, this attaches the TUI to that session over NATS (`botson.session.get`) and replays its prior turns into the transcript first (`internal/interface/tui/replay.go`), so it reads as if the conversation never stopped. A brand-new TUI session always runs under the fixed user `"tui"`, but resuming defaults `--user` to `"tui"` only as a starting guess -- a session created under a different user ID needs the matching `--user` explicitly, which `botson sessions list`'s `userId` column shows.

## Configuration reference

`~/.botsonv2/config.json`:
```json
{
  "model_name": "gemini-3.1-flash-lite",
  "gemini_api_key": "your_api_key_here",
  "root_agent": "Agent Botson",
  "workspace_dir": ""
}
```
- `workspace_dir` is only consulted by processes with no meaningful working directory of their own (the tray, launched via login autostart) — everything launched from a terminal (`core start`) uses its own actual cwd instead and ignores this field. The TUI's embedded core (see "Unified core architecture") doesn't consult it either -- being in-process, it simply runs in whatever directory the TUI itself was started from, with nothing to configure. Set once by `setup install` (defaulting to wherever install itself was run from); omitted (`""`) means "fall back to that process's own cwd."
- Read/write this file through `botson settings get/set` or the `updateSettings` tool rather than hand-editing while a `botson` process is running, so the in-memory copy that process is holding doesn't drift from disk — see "Self-configuration" above.

## Dependencies

Prefer the standard library where it can reasonably do the job; the project leans on these specific third-party packages rather than pulling in new ones casually:

- `google.golang.org/adk/v2` — core Agent Development Kit
- `google.golang.org/genai` — Gemini API client
- `github.com/nats-io/nats.go` + `github.com/nats-io/nats-server/v2` — the core's NATS API: `nats.go` is the client both sides use, `nats-server/v2` is embedded in-process by `botson core` so it stays a single binary with no external NATS server to run
- `github.com/spf13/cobra` — CLI command/flag framework powering `botson`
- `github.com/getlantern/systray` — cross-platform system tray icon (Windows `tray` subcommand)
- `golang.org/x/term` — masked (password-style) terminal input for `setup install`/`reset` prompts
- `gorm.io/gorm` (+ `glebarez/sqlite`) — ORM/SQLite backing session persistence
- `github.com/charmbracelet/{bubbletea,bubbles,lipgloss}` — the TUI

## Conventions

- Commit messages follow Conventional Commits style: `feat:`, `fix:`, `refactor:`, etc., imperative mood, no trailing period.
- Prefer adding a flag with a sensible default over introducing a new prompt, when a feature needs to be scriptable (see `--non-interactive` on `setup install`, `--force-full-uninstall`).
- Cobra commands that only manage a background process's lifecycle (not the agent runtime) set `PersistentPreRunE: noBootstrap` to skip the expensive config/Gemini/agent/session bootstrap — see `newCoreStartCmd`, `newCoreStopCmd`, etc.
- Import direction: `cmd/botson` → `internal/management` → `internal/agent` → `internal/tools` → `internal/config`. `internal/tools` must never import `internal/management` or `internal/agent` (it would cycle back through `internal/agent`'s import of `internal/tools`) — shared logic those layers both need (e.g. `Mask`) belongs in `internal/config` instead, not `internal/management`.
