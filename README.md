# ADK Agent Tool (v2)

This project is a Go-based application that implements an agent framework using Google's `ADK v2` (Agent Development Kit). It leverages the Gemini API to power an assistant agent capable of interacting with the local filesystem.

## Project Structure

*   `/core`: Main application logic.
    *   `main.go`: Application entry point, initializes the Gemini model and web server.
    *   `/agent`: Contains the `AssistantAgent` implementation and LLM instructions.
    *   `/config`: Handles application configuration, including environment variable loading.
    *   `/tools`: Contains specialized tools for the agent (e.g., `listFiles`, `readFile`).
*   `/bin`: Contains the compiled `app.exe` and its associated `.env` file.
*   `build.go`, `build.ps1`: Scripts for building the project.

## How It Works

The application follows a modular approach to agent development:

1.  **Agent Logic (`core/agent/`):** The `AssistantAgent` is built using `llmagent`. It is configured with system instructions and provided a set of tools that map to Go functions.
2.  **Tool Execution (`core/tools/`):** Tools are wrapped in `functiontool` interfaces, allowing the LLM to call them. Each tool (e.g., `ListFiles`, `ReadFile`) implements security checks to ensure file operations are restricted to the project workspace.
3.  **Configuration (`core/config/`):** The application automatically locates the `.env` file relative to the executable path to load necessary environment variables, such as `GEMINI_API_KEY`.
4.  **Runtime (`core/main.go`):** The application initializes the Gemini model, registers the `AssistantAgent`, and starts a web server (via `webLauncher`) that provides an interface to interact with the agent.

## Features

*   **Gemini-powered Agent:** Uses `gemini-3.1-flash-lite` to drive agent intelligence.
*   **Web-based Interface:** Provides a web UI and API for interacting with the agent.
*   **Agent Tools:** Pre-built, secure tools for file exploration.
*   **Configuration Management:** Loads API keys from a local `.env` file.

## Setup & Configuration

1.  **Environment Variables:**
    Create a `.env` file in the `bin/` directory with your Gemini API key:
    ```env
    GEMINI_API_KEY=your_api_key_here
    ```
2.  **Building:**
    Use the provided `build.ps1` to compile the application:
    ```powershell
    .\build.ps1
    ```
3.  **Running:**
    Execute the compiled binary from the `bin/` directory:
    ```powershell
    .\bin\app.exe
    ```

## Dependencies

*   `google.golang.org/adk/v2`: Core Agent Development Kit.
*   `google.golang.org/genai`: Google Gemini API client.
