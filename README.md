# Botson-ADKv2

This project is a Go-based application that implements an agent framework using Google's `ADK v2` (Agent Development Kit). It leverages the Gemini API to power an assistant agent capable of interacting with the local filesystem.

## Project Structure

*   `/cmd`: Contains project entry points.
    *   `/adk-web`: Web-based interface entry point.
*   `/core`: Main application logic.
    *   `/agent`: Contains the `AssistantAgent` implementation and LLM instructions.
    *   `/config`: Handles application configuration, including environment variable loading.
    *   `/tools`: Contains specialized tools for the agent (e.g., `listFiles`, `readFile`).
*   `/scripts`: Scripts for building the project (`build.go`).

## How It Works

The application follows a modular approach to agent development:

1.  **Agent Logic (`core/agent/`):** The `AssistantAgent` is built using `llmagent`. It is configured with system instructions and provided a set of tools that map to Go functions.
2.  **Tool Execution (`core/tools/`):** Tools are wrapped in `functiontool` interfaces, allowing the LLM to call them. Each tool (e.g., `ListFiles`, `ReadFile`) implements security checks to ensure file operations are restricted to the project workspace.
3.  **Configuration (`core/config/`):** The application locates environment variables, such as `GEMINI_API_KEY`.
4.  **Runtime (`cmd/`):** The project currently supports a web-based interface.

## Features

*   **Gemini-powered Agent:** Uses `gemini-3.1-flash-lite` to drive agent intelligence.
*   **Web/Desktop Interface:** Provides UI and API interfaces for interacting with the agent.
*   **Agent Tools:** Pre-built, secure tools for file exploration.
*   **Configuration Management:** Loads API keys from environment variables.

## Setup & Configuration

1.  **Environment Variables:**
    Ensure `GEMINI_API_KEY` is set in your environment:
    ```env
    GEMINI_API_KEY=your_api_key_here
    ```
2.  **Building:**
    Use the provided `scripts/` to compile the application:
    ```powershell
    go run scripts/build.go
    ```
3.  **Running:**
    Run the appropriate entry point:
    ```powershell
    go run cmd/adk-web/main.go
    ```

## Dependencies

*   `google.golang.org/adk/v2`: Core Agent Development Kit.
*   `google.golang.org/genai`: Google Gemini API client.
