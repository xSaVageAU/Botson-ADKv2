# AGENTS.md

This file orients AI coding agents (and human maintainers doing a deep dive) working in this codebase. For a short, human-facing project overview, see [README.md](./README.md).

Botson is a Go-based agent framework built on Google's **ADK v2**. As of 2026-07, it's one **core process** (the Gemini model, agent registry, session/artifact services) exposed over an embedded **NATS server**, and NATS is the *only* way anything talks to it. There is no TUI, no tray, and no CLI subcommand that manages agent/session/settings state directly — see "Unified core architecture" below for why, and what the two NATS subject namespaces cover.

---

## Project structure

- **`/cmd`**: application entry points. `botson-core` is the only one.
  - **[`/botson-core`](./cmd/botson-core/)**: the primary application (ships as `botson-<os>-<arch>`) — a small Cobra CLI with exactly two subcommands: `core` (the service itself, plus `start`/`stop`/`status`) and `setup install` (writes the initial `config.json`). Nothing else lives in this binary.
- **`/internal`**: main application packages.
  - **[`/agent`](./internal/agent/)**: custom recursive agent loader, default definitions, and tool registry.
  - **[`/artifact`](./internal/artifact/)**: local file system service for persistent artifacts.
  - **[`/config`](./internal/config/)**: `AppConfig` struct, load/save/update, and data-dir lookups (`~/.botson/`). `Load` caches a single shared instance per process and `Update` mutates it in place (see "Self-configuration" below) — this is the one package every settings-reading/writing code path ultimately goes through, so it can't import `internal/management`, `internal/agent`, or `internal/tools` without creating a cycle.
  - **[`/daemon`](./internal/daemon/)**: generic detach/control lifecycle (start/stop/status, PID files, the loopback control channel) for `core start`/`stop`/`status`.
  - **[`/setup`](./internal/setup/)**: backs `botson setup install` — the one local, direct-to-disk bootstrap step (Gemini API key, model, root agent), needed before any core or NATS server exists for a client to configure that over instead.
  - **[`/natsapi`](./internal/natsapi/)**: the server side of Botson's own NATS API — the `botson.*` subjects covering settings, custom-agent CRUD, and dashboard-shaped session listing/inspection. See `subjects.go` for the full subject table. The standard ADK surface (list-apps, sessions, running a turn, A2A) is a separate namespace, `adk.*`, fronted by `internal/adkgateway`; see "Unified core architecture" below.
  - **[`/adkgateway`](./internal/adkgateway/)**: spins up a real ADK REST server (`google.golang.org/adk/v2/cmd/launcher/prod`) on a loopback port and reverse-proxies `adk.*` NATS traffic to it, so REST/A2A behavior always matches upstream ADK exactly. Moved in from the sibling [`NATS-ADK-Proxy`](https://github.com/Savs-Agents/NATS-ADK-Proxy) repo, which only ever had this one consumer — that repo now just holds the wire `protocol` and a thin `client` package, both still used cross-repo (this package's `backend.go` sets the local ADK server's write/idle timeouts; see its doc comment for why the defaults are dangerous for a real multi-tool-call turn).
  - **[`/management`](./internal/management/)**: shared, interface-agnostic business logic (agents, sessions, config, dashboard) — the functions `internal/natsapi`'s handlers call. `ListSessions`/`GetSession`/`DeleteSession` (`sessions.go`) only need a `session.Service`, not the full Gemini/agent-loader bootstrap.
  - **[`/session`](./internal/session/)**: GORM & SQLite implementation for persisting conversation state. `InitPersistentSessionService` silences GORM's default logger (it writes to stdout, not stderr) at construction, since a consumer reading NATS replies off stdout would otherwise get corrupted output — don't reintroduce a per-consumer workaround for this. See [docs/sessions.md](./docs/sessions.md) for the full schema/API reference.
  - **[`/toolorder`](./internal/toolorder/)**: ADK plugin serializing a turn's parallel-dispatched tool calls into the order the model emitted them, across HITL confirmation pauses/resumes — including deferring non-gated calls (via synthetic, auto-approvable confirmations) behind gated ones. See the package doc and "HITL confirmation wire protocol" below.
  - **[`/automode`](./internal/automode/)**: background worker (started alongside `adk.*`/`botson.*` in `cmd/botson-core`'s `runCoreServer`) that keeps a session moving after every client disconnects. Polls the shared session service for sessions flagged `management.AutoModeStateKey`, finds any confirmation left pending, and answers it itself over NATS -- one more client of the standard `adk.*` run surface (via NATS-ADK-Proxy's own public `client` package), not a fork of it. See the package doc and "HITL confirmation wire protocol" below.
  - **[`/tools`](./internal/tools/)**: secure tools (`listFiles`, `readFile`, `writeFile`, `editFile`, `loadArtifacts`, `saveArtifact`, `updateSettings`, `runCommand`). `readFile`/`writeFile`/`editFile` share path validation via `resolveWorkspacePath` (`workspace.go`) — the one place that confines a tool to the workspace root and blocks `.env` access, so fix path-safety bugs there rather than per-tool.
  - **[`/procutil`](./internal/procutil/)**: `Run(ctx, name, args, opts)` — runs a subprocess with a timeout that actually works (kills the whole process group, not just the direct child) and truncates captured output. Leaf package (only depends on the stdlib), used by `internal/tools`' `runCommand` so this exec-safety logic exists in exactly one place.

## Architecture / how it works

1. **Registry loading**: default agents (bundled) and custom user agents (from `~/.botson/agents/`) are parsed and built recursively, supporting tool configuration and sub-agent delegation.
2. **Core hosting**: `botson core` runs an embedded NATS server (`nats-server/v2`, in-process, no external dependency) plus two subject namespaces on top of it — `adk.*` (`internal/adkgateway`) and `botson.*` (`internal/natsapi`). This is the one process that holds the agent registry, session service, and artifact service in memory.
3. **Every consumer is a NATS client.** There is no in-process interface of any kind in this repo.

## Unified core architecture

See [docs/process-architecture.md](./docs/process-architecture.md) for the full deep dive (process inventory, discovery mechanics, lifecycle diagrams, and known limitations) — this section is the condensed version.

Historically, `botson tui`, `botson web`, and `botson discord` were three fully independent OS processes, each running its own copy of `setupApp()`'s bootstrap (Gemini model, agent registry, session service) with no in-memory sharing. A 2026-07 redesign fixed that: one core process holds the state, first over HTTP, then over **NATS** — the core exposes exactly one API surface, not an ADK/HTTP-specific mechanism, so a Discord bot or web console can be built as a fully independent project needing only a NATS client. That redesign still shipped a TUI built into this same binary, including a fallback where it would silently become its own private, unregistered core if none was running. A later revision (this one) removed that: **there is no TUI, no tray, and no code path in this binary that runs a second copy of the agent runtime.** `botson` ships exactly `core` and `setup install`.

- **The core is `botson core`** (`cmd/botson-core/cmd_core.go`). `runCore` registers daemon state (`daemon.WriteState`, the loopback control channel) and calls `runCoreServer`, which does the actual work: start an embedded `*server.Server` (`nats-server/v2/server`) on a loopback port, wait for it to be ready, connect a `*nats.Conn` to it, then run two things concurrently against that connection via `errgroup` until `ctx` is cancelled: `adkproxy.New(...).Run(ctx)` (the imported NATS-ADK-Proxy, serving `adk.*`) and `natsapi.Serve(ctx, nc, boot.Launcher)` (serving `botson.*`). `runCore` always registers, regardless of how the process was launched — directly (`botson core`), detached (`core start`), or under an external supervisor like systemd.
- **Why two subject namespaces instead of one.** `adk.*` needs to match upstream ADK's REST/A2A behavior exactly (route set, CORS, telemetry, session semantics) — reimplementing that by hand (as the old `internal/interface/natscore` did, for a small subset: `agent.default`/`session.create`/`session.get`/`run`) means it can silently drift from what ADK itself does. Importing NATS-ADK-Proxy instead means Botson gets that surface for free, verified against the real `prod` launcher. `botson.*` is for the genuinely Botson-specific state (settings, custom-agent CRUD, dashboard aggregation) that was never part of ADK's own API and has to be hand-rolled somewhere regardless — `internal/natsapi` is that somewhere, mechanically wrapping `internal/management`/`internal/config` calls that used to be CLI-only.
- **`adk.*` does not stream yet.** NATS-ADK-Proxy's REST passthrough for `run` is request/reply only today (`run_sse`/A2A `message/stream` aren't implemented upstream in that package) — a caller gets a turn's full event list back at once rather than incrementally. This is a real behavior change from the old hand-rolled `botson.run` subject, which *did* stream one frame per event. Accepted for now since nothing in this repo needs live token-by-token updates; Botson inherits streaming automatically whenever NATS-ADK-Proxy adds it, since that's a shared dependency, not vendored code.
- **Known limitation, not solved by this**: switching an *already-running* core's workspace directory. It's pinned for that process's lifetime — restart it from a new directory to change it. True per-session/per-tool-call workspace switching would require threading a workspace argument through `agent.Context` and every tool built on `os.Getwd()`, a materially bigger change than this.

## Self-configuration

`internal/config.Load()` returns a single cached `*AppConfig` per process (not a fresh read each call), and `internal/config.Update(mutate func(*AppConfig))` edits that cached instance's fields **in place** before persisting to disk, rather than building a new struct and swapping the pointer. That means every long-lived holder of the config pointer within the core process (`cmd/botson-core`'s `appBoot.Config`) sees an `Update` immediately, with no restart needed. `botson.settings.set` (`internal/natsapi`) goes through `config.Update` for this reason — see `internal/config/config_test.go` for the regression test guarding this specifically (it would be easy to "simplify" `Update` back into load-then-replace and silently break this).

This is what makes the `updateSettings` agent tool (`internal/tools/update_settings.go`) meaningful: the running agent can change its own model/root-agent mid-conversation and have it actually take effect for the rest of that process's life, not just on next launch. It deliberately excludes secrets (the Gemini API key) — that stays human-controlled via `botson.settings.set` (or `setup install`), so a confused or compromised agent can't rotate or wipe its own credentials. `RequireConfirmation: true` is set on its registry entry (`internal/agent/registry.go`), same as `saveArtifact`, so it still pauses for a HITL approval before taking effect.

## Coding/exec tools

`writeFile` and `runCommand` (`internal/tools/write_file.go`, `internal/tools/run_command.go`) give the agent real editing and shell-execution capability in the project workspace, on top of the earlier read-only `readFile`/`listFiles`. Both default to `RequireConfirmation: true` in the registry, same posture as `saveArtifact`/`updateSettings` — this was a deliberate choice (2026-07) since it's the biggest capability jump in the tool registry so far, not because the code backing them is untrusted.

`runCommand` runs the given string through the platform's own shell (`/bin/sh -c` / `cmd /C`) in the workspace root, via `internal/procutil.Run` (timeout default 120s, output capped at ~200KB to protect the agent's own context). `procutil.Run` is the one place that handles two easy-to-get-wrong things correctly: killing the *whole process group* on timeout rather than just the direct child (`exec.CommandContext` alone would leave a forked-not-exec'd grandchild running, e.g. `sh -c "sleep 5"` on this box, holding the captured stdout/stderr pipe open past the shell's own death and silently defeating the timeout — see `internal/procutil/procutil_test.go`'s timeout case), and correctly classifying "killed by our own timeout" separately from "process ran and exited non-zero" (a SIGKILL'd process surfaces as the same `*exec.ExitError` type as a normal non-zero exit, so the timeout check has to run first).

**`editFile`** (`internal/tools/edit_file.go`, added 2026-07 mirroring how Claude Code's own Edit tool works) makes a precise find-and-replace edit — `oldString` must match the file's current content exactly, and exactly once unless `replaceAll` is set — rather than requiring `writeFile` to regenerate an entire file from memory just to change a few lines. `readFile` (`internal/tools/read_file.go`) was rewritten alongside it to return `cat -n`-style line-numbered, paginated output (`offset`/`limit`, default 2000-line limit) instead of the whole file as one string, so a line number it reports can be quoted directly in a following `editFile` call.

**Read-before-write guard** (`internal/tools/read_tracking.go`): `writeFile` and `editFile` both refuse to touch a file that hasn't been read via `readFile` earlier in the *same session* — except a brand-new file, which is exempt (nothing to have read). Tracking uses `agent.Context.State()`, verified to be a durable, session-scoped key/value store (traced through `agent/common_context.go` → the ADK runner's `StateDelta` → `session/database/service.go` → `internal/session/persistent.go`'s GORM/SQLite backing). **Key-shape matters here**: one flat state key per absolute path (`"botson:tools:read:" + fullPath` → `bool`), not one key holding a `map[string]bool` — session state is JSON round-tripped on reload, so a map value comes back as `map[string]interface{}` on a later turn while a bool round-trips losslessly as a bool either way. The guard fails open (silently skipped) if `ctx`/`ctx.State()` is nil, which only happens in hand-written unit tests, never in production tool invocation — see `internal/tools/fake_context_test.go`'s `fakeContext` (embeds `agent.ContextMock`, overrides `State()`) for the test double that lets tests actually exercise the guard instead of bypassing it.

Verified live end-to-end (not just unit-tested): drove a real conversation through the core asking the agent to edit a file. It chose `listFiles` → `readFile` (confirmed the state delta really contained `"botson:tools:read:<path>": true`) → `editFile` with a whitespace-exact `oldString`/`newString` pulled straight from the numbered read → paused for HITL confirmation (since `editFile` is `RequireConfirmation: true`) → after approval, the file was changed correctly with nothing else disturbed, and the agent even re-read the file on its own to confirm.

## HITL confirmation wire protocol

ADK's `RequireConfirmation: true` (used by `saveArtifact`, `updateSettings`, `writeFile`, `editFile`, `runCommand`) does **not** simply pause and resume the original tool call. Verified 2026-07 by driving a real `writeFile` call through the core directly and inspecting the raw persisted session -- the actual sequence for one gated call is:

1. The model's real `functionCall` (e.g. `writeFile`, some call id `X`).
2. An **immediate** `functionResponse` for that same call id `X`, before any human has done anything: `{"response": {"error": "error tool \"writeFile\" requires confirmation, please approve or reject"}}`. This is ADK's own internal bookkeeping for "this call is now blocked pending confirmation" -- it is not a real result, even though it has exactly the shape of one.
3. A synthetic wrapper `functionCall` named `adk_request_confirmation` (a **new**, different call id), whose `args.originalFunctionCall` embeds the real call (name, id `X`, args) and whose `args.toolConfirmation.hint` is the prompt to show the user. This is what a caller should actually render as "pending approval".
4. The human's decision arrives as a `functionResponse` on the `adk_request_confirmation` call id, `{"response": {"confirmed": true|false}}`.
5. Only *then*, if approved, does the real tool handler run -- producing a **second** `functionResponse` reusing the *original* call id `X`, this time with the tool's actual result.

**Ordering-only (deferred) confirmations.** ADK dispatches a turn's parallel `functionCall`s in concurrent goroutines, so a *non*-gated call (e.g. `readFile`) the model emitted *after* a gated one would really execute during step 2 -- before the gated call's approved effect exists. `internal/toolorder` closes that hole by pausing such a call through the exact same steps 2-5, with one difference: the wrapper's `args.toolConfirmation.payload` is `{"botsonToolOrderDeferred": true}` (`toolorder.DeferredPayloadKey`). That marker means the confirmation exists purely so the call rides the resume pass in its emitted position -- there is no human decision in it. **Every client consuming `adk.*` must check for it**: answer marked confirmations `{"confirmed": true}` immediately and silently, batched into the same `functionResponse` turn as the user's answers to that turn's real confirmations (Botson-TUI does this in its confirmation queue). Prompting a human for one is noise; never answering it stalls the run until the context deadline. Relatedly, a client must send *all* of a turn's confirmation answers in **one** message: the resume executes strictly in emitted order, so answering only a later call's confirmation while an earlier one is still unanswered leaves the resumed call waiting on a sibling that cannot resume yet.

**Auto mode.** A session can carry its own `management.AutoModeStateKey` (`"botson:autoMode"`) flag in durable state, toggled via `botson.sessions.setAutoMode` (never via `stateDelta`, since -- unlike `botson:cwd` -- it needs to change mid-conversation, not just on a fresh session's first turn). When set, every confirmation on that session is meant to be answered `{"confirmed": true}` without a human decision, marked with an extra key alongside `confirmed` in the response -- `{"confirmed": true, "botsonAutoMode": true}` -- so history (and any other client) can tell it apart from both a real human `y` and a `toolorder` ordering-only deferral. Two independent things answer these, on purpose: Botson-TUI answers immediately itself while connected (near-zero latency, see its `chat.go`), and `internal/automode`'s background worker polls every auto-mode session for anything still unanswered and answers it the same way -- a safety net that guarantees the turn keeps advancing even after every client disconnects, since neither NATS-ADK-Proxy nor the ADK module itself has any notion of "unattended." `internal/automode` also caps consecutive auto-approvals per session (see its `maxConsecutiveApprovals`) and turns the flag back off, with the reason recorded in-conversation, if a run looks like it's looping without ever seeing a fresh enable -- unattended does not mean unbounded.

The trap: call id `X` gets two different `functionResponse`s over the call's lifetime (the fake "requires confirmation" placeholder, then the real result), and naively keying a "call id → its result" lookup off the last-seen `functionResponse` works fine once everything's settled, but shows the tool call as falsely "Completed" with the placeholder error in the window between steps 2 and 5 -- i.e. exactly while the real `adk_request_confirmation` card is showing "pending". Any future NATS consumer building a UI on top of `adk.*` needs to apply the same fix this repo's now-removed TUI did: track which call ids appear as some `adk_request_confirmation`'s `args.originalFunctionCall.id`, and never render those ids' raw `functionCall` as their own trace at all -- their whole story (pending → approved/denied → result) belongs to the confirmation card alone.

## Platform-specific files

Windows-only functionality (`internal/daemon`'s detach mechanics, `internal/procutil`'s process-group kill) is split via Go build tags into `_windows.go` / `_unix.go` files (`detach_windows.go`/`detach_unix.go`, `procutil_windows.go`/`procutil_unix.go`). When adding a platform-specific feature, follow this pattern rather than runtime `if runtime.GOOS` branching inside shared files.

No Windows machine is available in this environment — changes touching either package are verified with `GOOS=windows GOARCH=amd64 go build ./...` cross-compilation only. There's no CI gate for this yet.

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
Interactive wizard: Gemini API key, then root agent (validated against `management.ListAgents()`, which needs no model/API key). Re-running later detects an existing config and asks before overwriting, so it doubles as a repair step. This is the *only* local, direct-to-disk configuration path — it exists solely because it has to run before any core/NATS server does. Everything else about running Botson (settings, agents, sessions) is a NATS subject; see `internal/natsapi/subjects.go`.

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

Any flag left unset falls back to whatever's already in `config.json` (or a built-in default on a brand-new install) rather than prompting — see `internal/setup/install.go`'s `InstallOptions`/`applyInstallOptions` for the exact precedence.

### Running the core

```bash
botson core --port=4222         # foreground: an embedded NATS server plus adk.*/botson.* subject handlers
botson core start --port=4222   # detached background process with a PID-file-backed lifecycle
botson core status               # reads the state file + probes the control channel
botson core stop [--force]      # graceful stop via control channel, or force-kill
```
Logs: `~/.botson/logs/core.log`. State: `~/.botson/core.pid`. Since Windows has no signal-based graceful shutdown for an arbitrary detached process, `stop` talks to a small loopback control channel the background process opens instead — this works identically on Linux. See [docs/process-architecture.md](./docs/process-architecture.md) for the full discovery/lifecycle mechanics.

### Everything else is a NATS subject, not a CLI command

Settings, custom-agent CRUD, and session/dashboard management are all `botson.*` subjects (`internal/natsapi`) — see that package's `subjects.go` for the full table, or [docs/sessions.md](./docs/sessions.md) for the session-specific ones. Creating/running/inspecting a session mid-conversation, or listing available apps, goes through `adk.*` (the imported NATS-ADK-Proxy) — see that package's README for its wire contract. There is no CLI equivalent for any of this anymore; a raw NATS client (or a short Go scratch script using `nats.go` directly) is the only way to exercise it outside of building a full consumer project. **See [docs/nats-api.md](./docs/nats-api.md) for the full consumer-facing reference** — every subject, request/reply shape, and a worked example.

## Configuration reference

`~/.botson/config.json`:
```json
{
  "model_name": "gemini-3.1-flash-lite",
  "gemini_api_key": "your_api_key_here",
  "provider": "gemini",
  "openrouter_api_key": "",
  "root_agent": "Agent Botson",
  "workspace_root": "/home/you/.botson/workspace",
  "nats_auth_token": "a generated hex token"
}
```
`provider` selects which `internal/providers` backend builds the model at
boot: `"gemini"` (default) or `"openrouter"`. `model_name` is interpreted
accordingly -- a bare Gemini model name, or a full OpenRouter model slug
(e.g. `"anthropic/claude-3.5-sonnet"`) when `provider` is `"openrouter"`,
in which case `openrouter_api_key` is required instead of (or alongside)
`gemini_api_key`. Like `model_name`/`root_agent`, changing `provider`
takes effect on the next core restart, not live.

`workspace_root` and `nats_auth_token` are generated automatically the
first time the config is loaded if either is missing (see
`fillWorkspaceAndToken` in `internal/config/config.go`) -- there's nothing
to set by hand on a fresh install. `nats_auth_token` gates every
connection to the embedded NATS server (`cmd/botson-core/cmd_core.go`) and
is deliberately never exposed through `botson.settings.get`/`Mask()` —
it's the credential gating that very API. `workspace_root` is the default
directory the file/command tools operate in (`internal/tools/workspace.go`);
a session can override it per-session via `stateDelta` on `/api/run` (see
[docs/nats-api.md](./docs/nats-api.md#setting-a-sessions-working-directory)),
to any absolute path — not sandboxed, unlike `workspace_root` itself.

Read/write this file through `botson setup install`, the `botson.settings.set` NATS subject, or the `updateSettings` tool rather than hand-editing while a `botson core` process is running, so the in-memory copy that process is holding doesn't drift from disk — see "Self-configuration" above.

## Dependencies

Prefer the standard library where it can reasonably do the job; the project leans on these specific third-party packages rather than pulling in new ones casually:

- `google.golang.org/adk/v2` — core Agent Development Kit
- `google.golang.org/genai` — Gemini API client
- `github.com/nats-io/nats.go` + `github.com/nats-io/nats-server/v2` — the core's NATS transport: `nats.go` is the client both `adk.*` and `botson.*` use, `nats-server/v2` is embedded in-process by `botson core` so it stays a single binary with no external NATS server to run
- `github.com/Savs-Agents/NATS-ADK-Proxy/client` — a thin, optional NATS client for `internal/automode`'s own in-process calls back into this core's `adk.*` surface; the gateway/backend that actually serves `adk.*` lives here, in `internal/adkgateway` (moved in from that sibling repo, whose scope is now just the wire protocol + this client)
- `github.com/spf13/cobra` — CLI command/flag framework powering `botson`'s two subcommands
- `golang.org/x/term` — masked (password-style) terminal input for `setup install` prompts
- `golang.org/x/sync` (`errgroup`) — runs `adk.*` and `botson.*` concurrently under one cancellable context in `runCoreServer`
- `gorm.io/gorm` (+ `glebarez/sqlite`) — ORM/SQLite backing session persistence

## Conventions

- Commit messages follow Conventional Commits style: `feat:`, `fix:`, `refactor:`, etc., imperative mood, no trailing period.
- Prefer adding a flag with a sensible default over introducing a new prompt, when a feature needs to be scriptable (see `--non-interactive` on `setup install`).
- Cobra commands that only manage a background process's lifecycle (not the agent runtime) set `PersistentPreRunE: noBootstrap` to skip the expensive config/Gemini/agent/session bootstrap — see `newCoreStartCmd`, `newCoreStopCmd`, `newSetupCmd`.
- Import direction: `cmd/botson-core` → `internal/natsapi` → `internal/management` → `internal/agent` → `internal/tools` → `internal/config`. `internal/tools` must never import `internal/management` or `internal/agent` (it would cycle back through `internal/agent`'s import of `internal/tools`) — shared logic those layers both need (e.g. `Mask`) belongs in `internal/config` instead, not `internal/management`.
