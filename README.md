# Botson

Botson is a personal AI agent console, built in Go on top of Google's **ADK v2** (Agent Development Kit) and the Gemini API. One binary gives you a terminal chat client backed by a shared core process, which exposes its agents, sessions, and tools over a NATS API — so other consumers (a Discord bot, a web console) can be built as fully independent projects against that same API.

It can read and manage files, hold persistent conversations, and ask for your approval before doing anything sensitive.

## Features

- **Terminal chat (TUI)** — the default experience, just run `botson`
- **A shared core** — one process holds the agent registry, session state, and Gemini model, exposed over an embedded NATS server so any client (this repo's TUI, or a separate project you build) can talk to it
- **Human-in-the-loop approvals** — sensitive tool calls pause for a yes/no confirmation
- **Custom agents** — define your own agents and tool sets, saved under `~/.botsonv2/agents/`
- **Background core** — the core can run detached, with `start`/`stop`/`status` and (on Windows) a system tray icon

## Getting started

You'll need a [Gemini API key](https://aistudio.google.com/apikey) and Go 1.26+ to build from source.

**1. Build**
```bash
go run scripts/build_linux.go     # or build_windows.go on Windows
```
This produces `bin/botsonv2-<os>-<arch>`.

**2. Install**
```bash
./bin/botsonv2-linux-amd64 setup install
```
An interactive wizard asks for your Gemini API key and a few other basics, then puts `botson` on your PATH.

**3. Run**
```bash
botson              # chat in your terminal (the default) -- auto-starts a private core if none is running
botson core start   # or start a shared, persistent core first, for any client to attach to
```

Run `botson --help` any time to see everything available.

## Configuration

Settings live in `~/.botsonv2/config.json` — your Gemini API key, chosen model, and root agent. You can hand-edit this file, redo `setup install`, or change it via `botson settings set`.

## Learn more

- **[AGENTS.md](./AGENTS.md)** — architecture, project layout, and the full CLI reference. Start here if you're contributing or maintaining the code (human or AI).
- **[docs/sessions.md](./docs/sessions.md)** — how sessions, state, and history are stored.
- **[docs/process-architecture.md](./docs/process-architecture.md)** — how Botson runs as one or more processes: the unified core, how clients discover it, and how the TUI/tray each fit in.
