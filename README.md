# Botson

Botson is a personal AI agent console, built in Go on top of Google's **ADK v2** (Agent Development Kit) and the Gemini API. One binary gives you three ways to talk to your agent — a terminal chat client, a web dashboard, and a Discord bot — all backed by the same agents, sessions, and tools.

It can read and manage files, hold persistent conversations, and ask for your approval before doing anything sensitive.

## Features

- **Terminal chat (TUI)** — the default experience, just run `botson`
- **Web console** — a dashboard for chatting, watching tool activity, and editing agent configs from a browser
- **Discord bot** — talk to your agent from Discord, with a whitelist and per-user sessions
- **Human-in-the-loop approvals** — sensitive tool calls pause for a yes/no confirmation, in the console or as a Discord DM
- **Custom agents** — define your own agents and tool sets, saved under `~/.botsonv2/agents/`
- **Background services** — the web console and Discord bot can run detached, with `start`/`stop`/`status` and (on Windows) a system tray icon

## Getting started

You'll need a [Gemini API key](https://aistudio.google.com/apikey) and Go 1.26+ to build from source.

**1. Build**
```bash
go run scripts/build_linux.go     # or build_windows.go on Windows
```
This produces `bin/botsonv2-<os>-<arch>` (plus the standalone `botsonv2-discord` and `botsonv2-adk` binaries).

**2. Install**
```bash
./bin/botsonv2-linux-amd64 setup install
```
An interactive wizard asks for your Gemini API key and a few other basics, then puts `botson` on your PATH.

**3. Run**
```bash
botson              # chat in your terminal (the default)
botson web          # open the web console at localhost:8080
botson discord      # run the Discord bot (needs a token, see below)
```

Run `botson --help` any time to see everything available.

## Configuration

Settings live in `~/.botsonv2/config.json` — your Gemini API key, chosen model, root agent, and (optional) Discord token. You can hand-edit this file, redo `setup install`, or change most of it from the web console's Settings tab.

## Learn more

- **[AGENTS.md](./AGENTS.md)** — architecture, project layout, and the full CLI reference. Start here if you're contributing or maintaining the code (human or AI).
- **[docs/sessions.md](./docs/sessions.md)** — how sessions, state, and history are stored.
- **[docs/process-architecture.md](./docs/process-architecture.md)** — how Botson runs as one or more processes: the unified core, how clients discover it, and how the TUI/Discord/tray each fit in.
