# Botson Workspace Console (ADK v2)

This repository implements a Go-based agent framework built on top of Google's **ADK v2** (Agent Development Kit). It leverages Gemini API to power an assistant agent capable of interacting with the local filesystem, managing custom configurations, and executing secure tools.

The project features a **Unified Workspace Console Single Page Application (SPA)** that combines system metrics, interactive chat logging, and agent configuration interfaces into a single dashboard.

---

## Project Structure

*   **`/cmd`**: Contains project application entry points.
    *   **[`/botson-prod`](./cmd/botson-prod/)**: Starts the production server (REST & A2A APIs) and serves the custom unified console SPA.
    *   **[`/botson-adk`](./cmd/botson-adk/)**: Starts the standard, built-in Google ADK developer console.
    *   **[`/agent-builder`](./cmd/agent-builder/)**: Boots a standalone local editor for quickly building and editing agent configurations.
*   **`/core`**: Main application packages.
    *   **[`/agent`](./core/agent/)**: Custom recursive agent loader, default definitions, and tool registry.
    *   **[`/artifact`](./core/artifact/)**: Local file system service for persistent artifacts.
    *   **[`/config`](./core/config/)**: Handles configurations and workspace path lookups.
    *   **[`/session`](./core/session/)**: GORM & SQLite implementation for persisting conversation states.
    *   **[`/tools`](./core/tools/)**: Secure tools (`listFiles`, `readFile`, `loadArtifacts`, `saveArtifact`).
    *   **[`/webui`](./core/webui/)**: Serves the unified SPA console. Includes embedded files, custom API handlers (`api_builder.go`, `api_dashboard.go`), and sublauncher routing.

---

## How It Works

The application provides a modular approach to building, running, and analyzing AI agents:

1.  **Registry Loading**: Default agents (bundled with system resources) and custom user agents (from `~/.botsonv2/agents/`) are parsed and built recursively. This allows configuring tools and sub-agent delegation.
2.  **Server Hosting**: The `botson-prod` application runs the ADK web server. It handles REST requests (`/api/*`), message stream runtimes (`/api/run_sse`), and serves the console SPA on `/botson/`.
3.  **Unified Frontend Console**: The browser SPA runs a single stylesheet and script architecture separated into modular concerns:
    *   `main`: Toggles active views and controls the app layout.
    *   `dashboard`: Displays metric summaries, agent lists, and session activities.
    *   `chat`: Provides real-time chat with streaming outputs, inline tool trace visualizations, and session state inspection.
    *   `builder`: Renders configuration forms to customize agents, prompt instructions, and tool sets.

---

## Features

*   **Unified Workspace Console SPA**: Instant view switching between Dashboard metrics, LLM Chat, and Agent Config Editor, preserving transient state.
*   **Concerns Separation**: Frontend code resides in CSS/JS modules (`main.css`, `dashboard.css`, `chat.css`, `builder.css`, and matching JS files). Backend endpoints are split into `api_dashboard.go` and `api_builder.go`.
*   **Frictionless CLI Flags**: Standard flags (like `-port 9000`) can be passed to `botson-prod` directly, while ADK-specific routing commands are automatically handled internally.
*   **Multi-Agent Registry & GORM Sessions**: Save custom agents dynamically to `~/.botsonv2/agents/` and maintain conversation states, artifacts, and telemetry spans in an SQLite db.

---

## Setup & Configuration

### 1. Environment Variables
Ensure `GEMINI_API_KEY` is set in your environment or a `.env` file at the root:
```env
GEMINI_API_KEY=your_api_key_here
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
*   **Standard ADK Developer Console** (Standard ADK internal console):
    ```powershell
    go run cmd/botson-adk/main.go
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
*   `gorm.io/gorm`: ORM backing SQL database persistence.
