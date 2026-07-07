# Botson Workspace Console (ADK v2)

This repository implements a Go-based agent framework built on top of Google's **ADK v2** (Agent Development Kit). It leverages Gemini API to power an assistant agent capable of interacting with the local filesystem, managing custom configurations, and executing secure tools.

The project features a **Unified Workspace Console Single Page Application (SPA)** that combines system metrics, interactive chat logging, and agent configuration interfaces into a single dashboard, alongside a fully integrated **Discord Gateway Bot** with built-in whitelisting and interactive tool confirmations.

---

## Project Structure

*   **`/cmd`**: Contains project application entry points. Three applications; `botson` is the primary one and the focus of ongoing design decisions, with `botson-discord` and `botson-adk` kept alongside it as minimal, single-purpose standalones.
    *   **[`/botson`](./cmd/botson/)**: The primary application (ships as `botsonv2-<os>-<arch>`) — a Cobra CLI with `tui` (default), `web`, `discord`, `tray` (Windows only), and `setup` subcommands, so one binary can boot a chat session, the custom web console + REST/A2A APIs, the Discord Gateway, a system tray icon, or install/uninstall/reset itself. Also the testing ground for `core/interface/tui` and `core/interface/web` now that their standalone binaries are gone. Does not include the built-in ADK web launcher.
    *   **[`/botson-discord`](./cmd/botson-discord/)**: Starts a standalone Discord Gateway bot listener — useful as a minimal, single-interface deployment.
    *   **[`/botson-adk`](./cmd/botson-adk/)**: Starts only the standard, built-in Google ADK developer console/web launcher and APIs — useful for testing against ADK's own built-in dev webserver, kept separate from `botson`.
*   **`/core`**: Main application packages.
    *   **[`/agent`](./core/agent/)**: Custom recursive agent loader, default definitions, and tool registry.
    *   **[`/artifact`](./core/artifact/)**: Local file system service for persistent artifacts.
    *   **[`/config`](./core/config/)**: Handles configurations and workspace path lookups.
    *   **[`/daemon`](./core/daemon/)**: Generic detach/control lifecycle (start/stop/status, PID files, the loopback control channel) shared by every backgroundable subcommand (`discord`, `web`, `tray`) in `botson`.
    *   **[`/setup`](./core/setup/)**: Backs `botson setup install/uninstall/reset/status` — interactive prompts, installing the binary to `~/.botsonv2/bin` and onto PATH, (Windows) tray-autostart registration, and a read-only status report of all of the above.
    *   **[`/interface`](./core/interface/)**: Unified system interfaces.
        *   **[`/web`](./core/interface/web/)**: Serves the unified SPA console. Includes embedded files, custom API handlers (`api_builder.go`, `api_dashboard.go`), and sublauncher routing.
        *   **[`/discord`](./core/interface/discord/)**: Coordinates the Discord Gateway listener, command handles (`commands.go`), security locks (`handlers.go`), database/disk session persistence (`sessions.go`), Human-in-the-Loop confirms (`hitl.go`), and disk-persisted pending-authorization requests (`pending.go`).
        *   **[`/tui`](./core/interface/tui/)**: Bubble Tea terminal chat interface. Callers assemble the agent/session/artifact plumbing and hand off to `tui.Run(...)`.
    *   **[`/management`](./core/management/)**: Shared, interface-agnostic business logic (agents, config, dashboard stats, Discord daemon control) callable from both the web API and CLI — so `botson` and the webui always drive the exact same functions.
    *   **[`/session`](./core/session/)**: GORM & SQLite implementation for persisting conversation states.
    *   **[`/tools`](./core/tools/)**: Secure tools (`listFiles`, `readFile`, `loadArtifacts`, `saveArtifact`).

---

## How It Works

The application provides a modular approach to building, running, and analyzing AI agents:

1.  **Registry Loading**: Default agents (bundled with system resources) and custom user agents (from `~/.botsonv2/agents/`) are parsed and built recursively. This allows configuring tools and sub-agent delegation.
2.  **Server Hosting**: The `botson` application runs the ADK web server. It handles REST requests (`/api/*`), message stream runtimes (`/api/run_sse`), and serves the console SPA on `/botson/`.
3.  **Discord Gateway listening**: The `botson-discord` application logs into Discord, registers slash commands (`/new`, `/list`, `/select`, `/info`, `/approve`), and starts checking incoming messages.
4.  **Unified Frontend Console**: The browser SPA runs a single stylesheet and script architecture separated into modular concerns:
    *   `main`: Toggles active views and controls the app layout.
    *   `dashboard`: Displays metric summaries, agent lists, and session activities.
    *   `chat`: Provides real-time chat with streaming outputs, inline tool trace visualizations, and session state inspection.
    *   `builder`: Renders configuration forms to customize agents, prompt instructions, and tool sets.

---

## Features

*   **Unified Workspace Console SPA**: Instant view switching between Dashboard metrics, LLM Chat, and Agent Config Editor, preserving transient state.
*   **Discord Gateway Integration**: Connect with your agent registry from anywhere. Whitelisted users can start multiple persistent chat sessions, view active session details, and select past histories with easy-to-use slash commands. The gateway is started/stopped as a background daemon — from the CLI (`discord start`/`stop`), the webui Settings tab's Start/Stop button, or the Windows tray icon — all three are just clients of the same background process.
*   **Interactive Human-in-the-Loop (HITL)**: Requires authorization confirmations before execution of specific tools. The bot renders interactive button elements to the console or Discord DMs so administrators can approve or deny actions dynamically.
*   **Concerns Separation**: Frontend code resides in CSS/JS modules (`main.css`, `dashboard.css`, `chat.css`, `builder.css`, and matching JS files). Backend endpoints are split into `api_dashboard.go` and `api_builder.go`.
*   **Multi-Purpose CLI**: `botson` is a Cobra-based CLI with `tui` (default), `web`, `discord`, `tray`, and `setup` subcommands, each with their own flags (e.g. `web --port 9000`), while ADK-specific routing commands are automatically handled internally.
*   **Background Services & System Tray**: `web` and `discord` can each run as a detached background process with `start`/`stop`/`status` subcommands, and (on Windows) a `tray` subcommand shows both as a system tray icon with one-click start/stop.
*   **First-Run Setup Wizard**: `setup install` interactively configures Botson and puts it on PATH; `setup uninstall`/`reset` handle teardown and starting over on config/data.
*   **Multi-Agent Registry & GORM Sessions**: Save custom agents dynamically to `~/.botsonv2/agents/` and maintain conversation states, artifacts, and telemetry spans in an SQLite db.

---

## Setup & Configuration

### 1. Application Configuration
All system-wide variables (Gemini API key, Discord token, and admin whitelisting) are configured in the `config.json` file located in the user configuration directory (`~/.botsonv2/config.json`). 

You can edit this file directly or update these properties dynamically using the **Settings** tab in the Workspace Console web interface.

A standard template looks like:
```json
{
  "model_name": "gemini-3.1-flash-lite",
  "gemini_api_key": "your_api_key_here",
  "root_agent": "Agent Botson",
  "discord": {
    "token": "your_discord_token_here",
    "owner_id": "your_discord_owner_user_id",
    "whitelist": []
  }
}
```
There is deliberately no `discord.enabled` flag: whether the gateway is running is controlled entirely by the `discord start`/`stop` background daemon (see below) — or the matching Start/Stop button in the webui Settings tab, which drives that same daemon.

Compile the platform-specific binaries into the `/bin` folder:
*   **Windows**:
    ```powershell
    go run scripts/build_windows.go
    ```
*   **Linux**:
    ```powershell
    go run scripts/build_linux.go
    ```

### 3. Running

The recommended first step on a new machine is `setup install` — an interactive wizard that asks for your Gemini API key, root agent, and (optional) Discord token/owner, then copies the binary to `~/.botsonv2/bin` and adds it to your PATH so plain `botson` works from any terminal afterward:
```powershell
./bin/botsonv2-windows-amd64.exe setup install
```
On Windows it also offers to register the tray icon to start automatically at login, and separately offers to start it right now. Re-running `install` later (e.g. after downloading a newer build) detects your existing configuration and asks before overwriting it, so it's safe to use as a repair/update step too. `setup status` (below) is the quickest way to confirm any of this actually took effect.

`botson` is a single multi-purpose CLI/TUI binary with subcommands, built on Cobra. Run it with no arguments (or `tui`) to boot straight into a chat session; use `web` or `discord` to run those interfaces instead:
```powershell
./bin/botsonv2-windows-amd64.exe             # same as `... tui` - interactive terminal chat
./bin/botsonv2-windows-amd64.exe tui --agent "Some Agent"
./bin/botsonv2-windows-amd64.exe web --port=8080
./bin/botsonv2-windows-amd64.exe discord      # foreground, tied to this terminal
./bin/botsonv2-windows-amd64.exe --help       # list all commands and flags
```

`discord` and `web` both also support running as a detached background process, independent of the terminal, with a PID-file-backed lifecycle so they can be checked on and stopped later:
```powershell
./bin/botsonv2-windows-amd64.exe discord start          # detach and run in the background
./bin/botsonv2-windows-amd64.exe discord status          # check if it's running
./bin/botsonv2-windows-amd64.exe discord stop            # ask it to shut down gracefully
./bin/botsonv2-windows-amd64.exe discord stop --force    # hard-kill if it won't respond

./bin/botsonv2-windows-amd64.exe web start --port=8080   # same lifecycle, for the web console
./bin/botsonv2-windows-amd64.exe web status
./bin/botsonv2-windows-amd64.exe web stop
```
Background logs go to `~/.botsonv2/logs/discord.log` and `~/.botsonv2/logs/web.log`, and lifecycle state to `~/.botsonv2/discord.pid` and `~/.botsonv2/web.pid`. Since Windows has no way to deliver a graceful shutdown signal to an arbitrary detached process, `stop` talks to a small loopback control channel the background process opens instead — this also works identically on Linux.

On Windows, `tray` puts an icon in the system tray that mirrors and controls both of the above — it polls the same state files `status` reads and calls the same `start`/`stop` logic directly, so it never needs to be running for the background services to keep working, and closing it never stops them:
```powershell
./bin/botsonv2-windows-amd64.exe tray
```
The tray menu offers "Start/Stop Discord" and "Start/Stop Web" toggles, a plain "Quit" that just removes the icon (services keep running), and a "Stop All & Quit" that gracefully stops both before exiting.

`tray` gets the exact same `start`/`stop`/`status` background lifecycle as `discord` and `web` — `tray start` launches it fully detached with no console window (so it can be put in the Windows Startup folder and appear silently on login), and `tray stop`/`status` let you control it from a terminal without having to right-click the icon:
```powershell
./bin/botsonv2-windows-amd64.exe tray start           # detach, no console window, icon appears
./bin/botsonv2-windows-amd64.exe tray status
./bin/botsonv2-windows-amd64.exe tray stop             # ask it to quit gracefully
./bin/botsonv2-windows-amd64.exe tray stop --force
```
Logs and state follow the same convention: `~/.botsonv2/logs/tray.log` and `~/.botsonv2/tray.pid`.

`setup` also has `uninstall`, `reset`, and `status`, rounding out the machine lifecycle alongside `install`:
```powershell
./bin/botsonv2-windows-amd64.exe setup uninstall         # confirm, optionally keep config.json, everything else deleted
./bin/botsonv2-windows-amd64.exe setup uninstall --full  # same, but also deletes config.json without asking
./bin/botsonv2-windows-amd64.exe setup reset             # interactive, per-category keep/replace
./bin/botsonv2-windows-amd64.exe setup status            # read-only report on install/PATH/autostart/daemon state
```
`uninstall` asks three separate yes/no questions — remove from PATH?, remove Startup/tray-autostart? (Windows), delete the installed binary? — so any one of them can be done alone (e.g. just take it off PATH, leaving the binary and autostart entry in place). Deleting the binary is the "real" uninstall step: only then does it stop any running `discord`/`web`/`tray` daemons and ask whether to keep `config.json`, deleting everything else under `~/.botsonv2` (sessions, custom agents, logs) either way. `--full` skips that keep-config question and deletes `config.json` too, for a complete wipe. `reset` asks per category ("Keep your Gemini API key?", "Keep your Discord settings?") whether to keep or immediately replace each one — reusing the same prompts `install` uses — and separately, defaulting to *no*, whether to also wipe session history and custom agents. Either way it ends with a valid, saved config, ready to run right away. `status` makes no changes — it reports whether the Gemini key/Discord/root agent are configured, whether the binary is installed and on PATH, whether tray autostart is registered, and whether `discord`/`web`/`tray` are currently running, which is handy for confirming a real install worked without digging through the registry or `~/.botsonv2` by hand.

The other two `cmd/` entry points remain standalone, single-purpose binaries:
*   **Standalone Discord Bot Gateway** (Discord integration listener only — a minimal, single-interface deployment):
	```powershell
	go run cmd/botson-discord/main.go
	```
*   **Standard ADK Developer Console** (Built-in ADK web launcher & APIs only — useful for testing against ADK's own built-in dev webserver):
	```powershell
	go run cmd/botson-adk/main.go
	```

---

## Dependencies

*   `google.golang.org/adk/v2`: Core Agent Development Kit.
*   `google.golang.org/genai`: Google Gemini API client.
*   `github.com/gorilla/mux`: Mux router for custom API routing.
*   `github.com/bwmarrin/discordgo`: Discord API bindings for Go.
*   `github.com/spf13/cobra`: CLI command/flag framework powering `botson`.
*   `github.com/getlantern/systray`: Cross-platform system tray icon (used for the Windows `tray` subcommand).
*   `golang.org/x/term`: Masked (password-style) terminal input for `setup install`/`reset`'s API key and Discord token prompts.
*   `gorm.io/gorm`: ORM backing SQL database persistence.
