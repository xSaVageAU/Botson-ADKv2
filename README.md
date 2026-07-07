# Botson Workspace Console (ADK v2)

This repository implements a Go-based agent framework built on top of Google's **ADK v2** (Agent Development Kit). It leverages Gemini API to power an assistant agent capable of interacting with the local filesystem, managing custom configurations, and executing secure tools.

The project features a **Unified Workspace Console Single Page Application (SPA)** that combines system metrics, interactive chat logging, and agent configuration interfaces into a single dashboard, alongside a fully integrated **Discord Gateway Bot** with built-in whitelisting and interactive tool confirmations.

---

## Project Structure

*   **`/cmd`**: Contains project application entry points.
    *   **[`/botson-prod`](./cmd/botson-prod/)**: Starts the production server (REST & A2A APIs) and serves the custom unified console SPA.
    *   **[`/botson-adk`](./cmd/botson-adk/)**: Starts the standard, built-in Google ADK developer console.
    *   **[`/botson-discord`](./cmd/botson-discord/)**: Starts the production Discord Gateway bot listener.
    *   **[`/botson-tui`](./cmd/botson-tui/)**: Renders a full terminal-based chat client (Bubble Tea SPA).
    *   **[`/agent-builder`](./cmd/agent-builder/)**: Boots a standalone local editor for quickly building and editing agent configurations.
*   **`/core`**: Main application packages.
    *   **[`/agent`](./core/agent/)**: Custom recursive agent loader, default definitions, and tool registry.
    *   **[`/artifact`](./core/artifact/)**: Local file system service for persistent artifacts.
    *   **[`/config`](./core/config/)**: Handles configurations and workspace path lookups.
    *   **[`/interface`](./core/interface/)**: Unified system interfaces.
        *   **[`/web`](./core/interface/web/)**: Serves the unified SPA console. Includes embedded files, custom API handlers (`api_builder.go`, `api_dashboard.go`), and sublauncher routing.
        *   **[`/discord`](./core/interface/discord/)**: Coordinates the Discord Gateway listener, command handles (`commands.go`), security locks (`handlers.go`), database/disk session persistence (`sessions.go`), and Human-in-the-Loop confirms (`hitl.go`).
    *   **[`/session`](./core/session/)**: GORM & SQLite implementation for persisting conversation states.
    *   **[`/tools`](./core/tools/)**: Secure tools (`listFiles`, `readFile`, `loadArtifacts`, `saveArtifact`).

---

## How It Works

The application provides a modular approach to building, running, and analyzing AI agents:

1.  **Registry Loading**: Default agents (bundled with system resources) and custom user agents (from `~/.botsonv2/agents/`) are parsed and built recursively. This allows configuring tools and sub-agent delegation.
2.  **Server Hosting**: The `botson-prod` application runs the ADK web server. It handles REST requests (`/api/*`), message stream runtimes (`/api/run_sse`), and serves the console SPA on `/botson/`.
3.  **Discord Gateway listening**: The `botson-discord` application logs into Discord, registers slash commands (`/new`, `/list`, `/select`, `/info`, `/approve`), and starts checking incoming messages.
4.  **Unified Frontend Console**: The browser SPA runs a single stylesheet and script architecture separated into modular concerns:
    *   `main`: Toggles active views and controls the app layout.
    *   `dashboard`: Displays metric summaries, agent lists, and session activities.
    *   `chat`: Provides real-time chat with streaming outputs, inline tool trace visualizations, and session state inspection.
    *   `builder`: Renders configuration forms to customize agents, prompt instructions, and tool sets.

---

## Features

*   **Unified Workspace Console SPA**: Instant view switching between Dashboard metrics, LLM Chat, and Agent Config Editor, preserving transient state.
*   **Discord Gateway Integration**: Connect with your agent registry from anywhere. Whitelisted users can start multiple persistent chat sessions, view active session details, and select past histories with easy-to-use slash commands.
*   **Interactive Human-in-the-Loop (HITL)**: Requires authorization confirmations before execution of specific tools. The bot renders interactive button elements to the console or Discord DMs so administrators can approve or deny actions dynamically.
*   **Concerns Separation**: Frontend code resides in CSS/JS modules (`main.css`, `dashboard.css`, `chat.css`, `builder.css`, and matching JS files). Backend endpoints are split into `api_dashboard.go` and `api_builder.go`.
*   **Frictionless CLI Flags**: Standard flags (like `-port 9000`) can be passed to `botson-prod` directly, while ADK-specific routing commands are automatically handled internally.
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
    "enabled": true,
    "token": "your_discord_token_here",
    "owner_id": "your_discord_owner_user_id",
    "whitelist": []
  }
}
```

### 2. Building
Compile all platform binaries into the `/bin` folder using the build script:
```powershell
go run scripts/build.go
```

### 3. Running
Run the appropriate entry point depending on your use case:
*   **Production Server & Unified Console** (REST APIs + Unified UI on port `:8080`):
	```powershell
	go run cmd/botson-prod/main.go -port=8080
	```
*   **Production Discord Bot Gateway** (Discord integration listener):
	```powershell
	go run cmd/botson-discord/main.go
	```
*   **Standard ADK Developer Console** (Standard ADK internal console):
	```powershell
	go run cmd/botson-adk/main.go
	```
*   **Interactive Terminal Console (TUI)** (Bubble Tea command-line interface):
	```powershell
	go run cmd/botson-tui/main.go
	```
*   **Standalone Agent Config Builder** (Lightweight configuration editor on port `:8081`):
	```powershell
	go run cmd/agent-builder/main.go
	```

---

## Dependencies

*   `google.golang.org/adk/v2`: Core Agent Development Kit.
*   `google.golang.org/genai`: Google Gemini API client.
*   `github.com/gorilla/mux`: Mux router for custom API routing.
*   `github.com/bwmarrin/discordgo`: Discord API bindings for Go.
*   `gorm.io/gorm`: ORM backing SQL database persistence.
