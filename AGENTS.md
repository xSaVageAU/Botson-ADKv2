# AGENTS.md

This file orients AI coding agents (and human maintainers doing a deep dive) working in this codebase. For a short, human-facing project overview, see [README.md](./README.md).

Botson is a Go-based agent framework built on Google's **ADK v2**, exposing the same agent/session/artifact backend through three interfaces (TUI, web console, Discord) from one primary binary, `botson`.

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
  - **[`/daemon`](./core/daemon/)**: generic detach/control lifecycle (start/stop/status, PID files, the loopback control channel) shared by every backgroundable subcommand (`discord`, `web`, `tray`).
  - **[`/setup`](./core/setup/)**: backs `botson setup install/uninstall/reset/status` — prompts (interactive and flag-driven), installing the binary to `~/.botsonv2/bin` and onto PATH, (Windows) tray-autostart registration, and a read-only status report.
  - **[`/interface`](./core/interface/)**: the three user-facing interfaces.
    - **[`/web`](./core/interface/web/)**: serves the unified SPA console — embedded files, custom API handlers (`api_builder.go`, `api_dashboard.go`), sublauncher routing.
    - **[`/discord`](./core/interface/discord/)**: Discord Gateway listener, command handlers (`commands.go`), security locks (`handlers.go`), DB/disk session persistence (`sessions.go`), HITL confirms (`hitl.go`), disk-persisted pending-authorization requests (`pending.go`).
    - **[`/tui`](./core/interface/tui/)**: Bubble Tea terminal chat interface. Callers assemble the agent/session/artifact plumbing and hand off to `tui.Run(...)`.
  - **[`/management`](./core/management/)**: shared, interface-agnostic business logic (agents, config, dashboard stats, Discord daemon control) callable from both the web API and the CLI, so `botson` and the webui always drive the exact same functions.
  - **[`/session`](./core/session/)**: GORM & SQLite implementation for persisting conversation state. See [docs/sessions.md](./docs/sessions.md) for the full schema/API reference.
  - **[`/tools`](./core/tools/)**: secure tools (`listFiles`, `readFile`, `loadArtifacts`, `saveArtifact`, `updateSettings`).

## Architecture / how it works

1. **Registry loading**: default agents (bundled) and custom user agents (from `~/.botsonv2/agents/`) are parsed and built recursively, supporting tool configuration and sub-agent delegation.
2. **Server hosting**: `botson web` runs the ADK web server — REST (`/api/*`), streaming (`/api/run_sse`), and the console SPA on `/botson/`.
3. **Discord gateway**: logs into Discord, registers slash commands (`/new`, `/list`, `/select`, `/info`, `/approve`), listens for incoming messages.
4. **Web console frontend**: single stylesheet/script architecture split into `main` (layout/view switching), `dashboard` (metrics, agent lists, session activity), `chat` (streaming chat, tool trace visualization, session inspection), `builder` (agent/prompt/tool config forms).

## Bare `botson` dispatch

A bare `botson` (no subcommand) runs whichever interface `config.AppConfig.DefaultCommand` names (`"tui"` / `"web"` / `"discord"`), via `runDefaultCommand` in `cmd/botson/main.go`. Empty or unrecognized values fall back to `"tui"`. This field is **not yet exposed** through `setup install` or any prompt — it's config.json-only for now, set by hand.

## Self-configuration

`core/config.Load()` returns a single cached `*AppConfig` per process (not a fresh read each call), and `core/config.Update(mutate func(*AppConfig))` edits that cached instance's fields **in place** before persisting to disk, rather than building a new struct and swapping the pointer. That means every long-lived holder of the config pointer within one process (`cmd/botson`'s `appBoot.Config`, anything else that called `Load()` earlier) sees an `Update` immediately, with no restart needed. `core/management.UpdateConfig` and `botson settings set` both go through `config.Update` for this reason — see `core/config/config_test.go` for the regression test guarding this specifically (it would be easy to "simplify" `Update` back into load-then-replace and silently break this).

This is what makes the `updateSettings` agent tool (`core/tools/update_settings.go`) meaningful: the running agent can change its own model/root-agent/default-command mid-conversation and have it actually take effect for the rest of that process's life, not just on next launch. It deliberately excludes secrets (Gemini API key, Discord token/owner) — those stay human-controlled via `botson settings set` or the web console, so a confused or compromised agent can't rotate or wipe its own credentials. `RequireConfirmation: true` is set on its registry entry (`core/agent/registry.go`), same as `saveArtifact`, so it still pauses for a HITL approval before taking effect.

## Platform-specific files

Windows-only functionality (tray icon, autostart registration, uninstall self-delete helper) is split via Go build tags into `_windows.go` / `_unix.go` / `_other.go` files (e.g. `tray_windows.go` vs `tray_other.go`, `autostart_windows.go` vs `autostart_unix.go`). When adding a platform-specific feature, follow this pattern rather than runtime `if runtime.GOOS` branching inside shared files, except where the branch is small and genuinely one-off (e.g. the tray-specific prompts inside `core/setup/install.go`'s `Install`).

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
botson tui --agent "Some Agent"
botson web --port=8080
botson discord                  # foreground, tied to this terminal
botson --help                   # list all commands and flags
```

`discord` and `web` also run as detached background processes with a PID-file-backed lifecycle:
```bash
botson discord start / status / stop [--force]
botson web start --port=8080 / status / stop [--force]
```
Logs: `~/.botsonv2/logs/{discord,web}.log`. State: `~/.botsonv2/{discord,web}.pid`. Since Windows has no signal-based graceful shutdown for an arbitrary detached process, `stop` talks to a small loopback control channel the background process opens instead — this works identically on Linux.

On Windows, `tray` mirrors and controls both via the same state files/logic (`tray`, `tray start/status/stop [--force]`) — closing the tray never stops the background services, since it's just another client of the same daemon state.

### Setup lifecycle: uninstall / reset / status

```bash
botson setup uninstall                        # ask per step: PATH, startup, binary, keep config.json?
botson setup uninstall --force-full-uninstall # skip every prompt, completely wipe ~/.botsonv2
botson setup reset                            # interactive, per-category keep/replace
botson setup status                           # read-only report on install/PATH/autostart/daemon state
```
`uninstall` asks up to three yes/no questions (remove from PATH? remove Startup/tray-autostart on Windows? delete the installed binary?) so any one can be done alone. Deleting the binary is the "real" uninstall step — it stops any running `discord`/`web`/`tray` daemons first, then asks whether to keep `config.json` (deleting sessions/custom agents/logs either way). `--force-full-uninstall` skips all prompts and wipes everything including `config.json`.

`reset` asks per-category ("keep your Gemini API key?", "keep your Discord settings?") whether to keep or replace, reusing `install`'s own prompt functions, and separately (defaulting to *no*) whether to wipe session history and custom agents. Always ends with a valid, saved config.

`status` makes no changes — reports whether the Gemini key/Discord/root agent are configured, whether the binary is installed and on PATH, tray autostart registration, and whether `discord`/`web`/`tray` are currently running.

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
  "default_command": ""
}
```
- No `discord.enabled` field, deliberately — whether the gateway runs is controlled entirely by the `discord start`/`stop` daemon (or the webui's Start/Stop button, which drives the same daemon).
- `default_command` (`""` / `"tui"` / `"web"` / `"discord"`) picks what a bare `botson` runs; see "Bare `botson` dispatch" above. Settable via `botson settings set --default-command` or the `updateSettings` agent tool; not yet exposed via `setup install`.
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
