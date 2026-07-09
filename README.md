# Botson

Botson is an AI agent service, built in Go on top of Google's **ADK v2** (Agent Development Kit) and the Gemini API. It's a single core process — never more than one — that holds the agent registry, session state, and Gemini model, and exposes all of it over NATS. There is no built-in chat UI: every consumer (a Discord bot, a web console, a terminal client) is a separate project that talks to the one running core purely over NATS, so there's never a case of "the Discord bot has one agent instance and the web UI started a second one."

It can read and manage files, hold persistent conversations, and ask for approval before doing anything sensitive.

## Features

- **One core, NATS-only** — `botson core` is the only process that ever holds the agent runtime; nothing about running Botson requires a specific frontend
- **Standard ADK surface over NATS** — list-apps, sessions, running a turn, and A2A, fronted by an imported [NATS-ADK-Proxy](https://github.com/Savs-Agents/NATS-ADK-Proxy) under the `adk.` subject prefix, matching upstream ADK's own REST/A2A behavior exactly
- **Botson-specific state over NATS too** — settings, custom-agent CRUD, and dashboard-shaped session listing, under the `botson.` subject prefix (`internal/natsapi`) — nothing requires touching `~/.botsonv2/` files directly
- **Human-in-the-loop approvals** — sensitive tool calls pause for a yes/no confirmation
- **Custom agents** — define your own agents and tool sets, saved under `~/.botsonv2/agents/`
- **Background core** — runs detached, with `start`/`stop`/`status`, or under a real service supervisor (systemd, etc.)

## Getting started

You'll need a [Gemini API key](https://aistudio.google.com/apikey) and Go 1.26+ to build from source.

**1. Build**
```bash
go run scripts/build_linux.go     # or build_windows.go on Windows
```
This produces `bin/botsonv2-<os>-<arch>`.

**2. Configure**
```bash
./bin/botsonv2-linux-amd64 setup install
```
An interactive wizard asks for your Gemini API key and root agent, and writes `~/.botsonv2/config.json`. This is the only step that isn't a NATS call — it has to run before any core exists for a client to configure that over.

**3. Run the core**
```bash
botson core start   # or `botson core` to run in the foreground
```
From here, talk to it over NATS — see `internal/natsapi/subjects.go` for the `botson.*` subject table and [NATS-ADK-Proxy](https://github.com/Savs-Agents/NATS-ADK-Proxy)'s README for the `adk.*` surface. `botson --help` lists the CLI's two subcommands (`core`, `setup`); there is no third.

## Configuration

Settings live in `~/.botsonv2/config.json` — your Gemini API key, chosen model, and root agent. Change it via `setup install`, the `botson.settings.set` NATS subject, or (for everything but the API key) the agent's own `updateSettings` tool.

## Learn more

- **[AGENTS.md](./AGENTS.md)** — architecture, project layout, and the full CLI reference. Start here if you're contributing or maintaining the code (human or AI).
- **[docs/sessions.md](./docs/sessions.md)** — how sessions, state, and history are stored, and the NATS subjects for reading them.
- **[docs/process-architecture.md](./docs/process-architecture.md)** — how Botson runs as a process: the core, its two NATS subject namespaces, and how a client discovers a running one.
