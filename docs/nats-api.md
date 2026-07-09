# Building a consumer: the NATS API reference

This is the reference for writing your own consumer of a running `botson
core` — a Discord bot, a web UI, a CLI, anything. The core's *only*
interface is NATS; there is no HTTP, no gRPC, no Go package to import. If
you can open a NATS connection and publish/subscribe, you can build a
consumer in any language.

See [docs/process-architecture.md](./process-architecture.md) for how the
core process itself is structured; this document is scoped to "what do I
send, and what do I get back."

---

## 1. Connecting

The core runs an **embedded NATS server** — there's nothing else to install.
Point a NATS client at it:

```
nats://<host>:<port>
```

`<port>` defaults to `4222` (`botson core --port=4222` / `core start
--port=4222`). If you don't control how the core was started and need to
discover its actual port, read `~/.botson/core.pid` — it's a JSON file
with a `meta.natsPort` field (see [process-architecture.md §4](./process-architecture.md#4-how-a-client-finds-or-announces-a-core)).
In the common case (you're running your own core alongside your consumer,
or you know the port because you configured it) you can skip this and just
hardcode the port you started it with.

The embedded server requires a token on every connection — `setup install`
generates one the first time a core runs and stores it in
`~/.botson/config.json`'s `nats_auth_token` field. Read it from there and
pass it when connecting:

```go
nc, _ := nats.Connect("nats://127.0.0.1:4222", nats.Token(token))
```

If your consumer runs on the same machine as the core, this is the only
file you need to read to pair with zero configuration -- Botson-TUI does
exactly this. For a remote core, the token has to reach you out of band
(the operator copies it from their own `~/.botson/config.json`, or from
`setup install`'s output when the core was first configured); there's no
way to fetch it over NATS itself, since it's the credential gating that
very connection. See
[process-architecture.md §8](./process-architecture.md#8-limitations-and-directions-worth-thinking-about)
for what this auth model does and doesn't cover (still no TLS, still binds
to `127.0.0.1` by convention rather than enforcement).

Once connected, everything below is either a **request/reply** call
(`nats.Request`/`RequestMsg` — publish, get exactly one reply) or, for a
couple of `adk.*` cases, a plain subscribe/publish. Every subject in this
document is request/reply unless stated otherwise.

---

## 2. Two subject namespaces

| Prefix | What it is | Implemented by |
|---|---|---|
| `adk.*` | The standard ADK REST/A2A surface: list agents, create/get sessions, run a turn, A2A JSON-RPC. Matches upstream Google ADK behavior exactly. | An imported [`github.com/Savs-Agents/NATS-ADK-Proxy`](https://github.com/Savs-Agents/NATS-ADK-Proxy) — not code in this repo. |
| `botson.*` | Everything Botson-specific that isn't part of ADK's own API: settings, custom-agent CRUD, dashboard-shaped session listing/aggregation. | `internal/natsapi` (this repo). |

Use `adk.*` for anything about actually running the agent (sessions, turns,
A2A). Use `botson.*` for managing the Botson instance itself (its config,
its custom agents, browsing session history for a dashboard-style view).

---

## 3. `adk.*` — running the agent

Full protocol details live in NATS-ADK-Proxy's own
[README](https://github.com/Savs-Agents/NATS-ADK-Proxy#wire-protocol) and
its `protocol` package godoc — this section is the practical subset you
need to drive a chat.

**Shape**: request/reply, no envelope. The request/response *body* is the
raw HTTP request/response body ADK's REST API would have used — JSON in,
JSON out — carried via NATS message `Data`. HTTP semantics that don't fit
in a body (method, path, headers, status) ride on NATS message **headers**
instead:

| Header | Where | Meaning |
|---|---|---|
| `Adk-Path` | Request | REST only — path + query, e.g. `/api/list-apps` |
| `Adk-Hdr-<Name>` | Request or response | A forwarded HTTP header, e.g. `Adk-Hdr-Content-Type`, `Adk-Hdr-Authorization` |
| `Adk-Status` | Response | HTTP status code, decimal string |
| `Adk-Gateway-Error` | Response | Set only if the *gateway itself* failed (backend unreachable, bad method, timeout) — distinct from the backend returning a real HTTP error status. Check this before trusting `Adk-Status`. |

### Subjects

| Subject | Maps to |
|---|---|
| `adk.rest.GET` / `.POST` / `.PUT` / `.PATCH` / `.DELETE` | `/api/*`, method + `Adk-Path` header select the route |
| `adk.a2a.v1.invoke` | `POST /a2a/v1/invoke` (A2A 1.0 JSON-RPC) |
| `adk.a2a.v0.invoke` | `POST /a2a/invoke` (A2A 0.3 compat JSON-RPC) |
| `adk.a2a.agentcard` | `GET /.well-known/agent-card.json` |

### Common REST paths (via `Adk-Path`)

| Path | Method | What it does |
|---|---|---|
| `/api/list-apps` | GET | List available agent names |
| `/api/apps/{app}/users/{user}/sessions/{sessionId}` | POST | Create a session (empty body) |
| `/api/apps/{app}/users/{user}/sessions/{sessionId}` | GET | Get a session (state + event history) |
| `/api/apps/{app}/users/{user}/sessions/{sessionId}` | DELETE | Delete a session |
| `/api/run` | POST | Run one turn; body below |

`{app}` is an agent name (from `list-apps` or `botson.agents.list`).
`{user}` is **entirely yours to choose** — the core makes no assumptions
about user identity; see §5.

**`POST /api/run` request body**:
```json
{
  "appName": "Agent Botson",
  "userId": "your-consumer:some-id",
  "sessionId": "<from create-session>",
  "newMessage": { "role": "user", "parts": [{ "text": "hello" }] }
}
```
`newMessage` is a `genai.Content` — Google's Gemini content type
(role + parts; parts can be text, function calls, function responses,
etc.).

**Response** (status 200): a JSON array of `session.Event`-shaped objects,
the full turn's events at once — `[{"author": "...", "content": {...}}, ...]`.
There is currently **no streaming** (`run_sse`/A2A `message/stream` aren't
implemented in NATS-ADK-Proxy yet) — you get the whole turn back in one
reply, not incrementally. See
[AGENTS.md](../AGENTS.md#unified-core-architecture) for the tracking note
on this.

### Example (Go, using `nats.go` directly — no client library required)

```go
nc, _ := nats.Connect("nats://127.0.0.1:4222")

// list-apps
req := nats.NewMsg("adk.rest.GET")
req.Header.Set("Adk-Path", "/api/list-apps")
reply, _ := nc.RequestMsg(req, 5*time.Second)
// reply.Header.Get("Adk-Status") == "200"
// reply.Data == `["Agent Botson"]`

// create a session
req = nats.NewMsg("adk.rest.POST")
req.Header.Set("Adk-Path", "/api/apps/Agent Botson/users/myapp:123/sessions/abc-1")
nc.RequestMsg(req, 5*time.Second)

// run a turn
body := `{"appName":"Agent Botson","userId":"myapp:123","sessionId":"abc-1",
          "newMessage":{"role":"user","parts":[{"text":"hi"}]}}`
req = nats.NewMsg("adk.rest.POST")
req.Header.Set("Adk-Path", "/api/run")
req.Header.Set("Adk-Hdr-Content-Type", "application/json")
req.Data = []byte(body)
reply, _ = nc.RequestMsg(req, 60*time.Second)
// reply.Data == `[{"author":"Agent Botson","content":{...}}, ...]`
```

(Go consumers can also use NATS-ADK-Proxy's own optional `client` package
instead of building requests by hand — see its README. Non-Go consumers
implement the same header/subject contract directly; it's plain NATS, not
Go-specific.)

### Human-in-the-loop (HITL) confirmations

If a tool call requires approval (`RequireConfirmation: true` in Botson's
tool registry), a run's events include a synthetic
`adk_request_confirmation` function call instead of the real tool result —
your consumer needs to render that as a pending-approval prompt and reply
with a `functionResponse` of `{"confirmed": true|false}` on a following
turn. This is ADK's own wire format, unrelated to NATS. See
[AGENTS.md — "HITL confirmation wire protocol"](../AGENTS.md#hitl-confirmation-wire-protocol)
for the exact event sequence and the trap to avoid (a call ID gets two
different `functionResponse`s over its lifetime — don't key off "last seen"
alone).

### Setting a session's working directory

The file/command tools (`listFiles`, `readFile`, `writeFile`, `editFile`,
`runCommand`) default to the core's configured workspace
(`workspace_root` in settings, below). A session can override this by
sending upstream ADK's own `stateDelta` field on a `POST /api/run`
request — it's not part of Botson's own wire format, just a real,
already-functional field on the request body NATS-ADK-Proxy forwards
byte-for-byte:

```json
{
  "appName": "Agent Botson",
  "userId": "your-consumer:some-id",
  "sessionId": "abc-1",
  "newMessage": { "role": "user", "parts": [{ "text": "hello" }] },
  "stateDelta": { "botson:cwd": "/path/to/a/project" }
}
```

Once set, every tool call in that session uses `/path/to/a/project`
instead of the default workspace, for the rest of the session's life (no
need to resend it on later turns). It's typically sent once, on a
session's first turn. **This path is not sandboxed** — it can be anywhere
the core process can read/write, which is why the embedded NATS server
requires a token (§1): only a holder of that token can point a session's
tools at an arbitrary host path.

---

## 4. `botson.*` — managing the Botson instance

Defined in [`internal/natsapi/subjects.go`](../internal/natsapi/subjects.go).
Every subject here is plain request/reply: publish JSON (or, for
no-argument subjects, an empty body), get one JSON reply. Every reply type
includes an `"error"` field, present only on failure — check for it before
using the rest of the reply.

### Settings

| Subject | Request | Reply |
|---|---|---|
| `botson.settings.get` | *(empty)* | `{"model_name","gemini_api_key","root_agent","workspace_root","provider","openrouter_api_key"}` — `gemini_api_key`/`openrouter_api_key` are always masked (`"******"`); `nats_auth_token` is never included here at all |
| `botson.settings.set` | `{"modelName"?, "rootAgent"?, "geminiApiKey"?, "workspaceRoot"?, "provider"?, "openRouterApiKey"?}` — omit a field to leave it unchanged | same shape as `settings.get`, reflecting the new values. A `workspaceRoot` change applies immediately, no restart needed; `modelName`/`provider`/the API keys take effect on the next core restart |

### Agents

| Subject | Request | Reply |
|---|---|---|
| `botson.agents.list` | *(empty)* | `[{"name","description","is_root","private","tools":[...],"instructions","read_only"}, ...]` |
| `botson.agents.tools` | *(empty)* | `{"standard":["listFiles","readFile",...], "agents":["Agent Botson",...]}` — valid values for an agent's `tools` list (built-in tools, plus any other agent name for sub-agent delegation) |
| `botson.agents.save` | `{"name","description","tools":[...],"private","instructions"}` | `{}` on success. Creates or overwrites a custom agent under `~/.botson/agents/<name>/`. If `name` collides with a bundled default agent, saves as a user override. |
| `botson.agents.delete` | `{"name"}` | `{}` on success. Only affects custom user agents — bundled defaults can't be deleted. |

### Sessions (dashboard-shaped view)

Distinct from `adk.rest.*`'s raw session objects — these are shaped for
display (extracted display name, human-readable event summaries) rather
than for driving a conversation. A session's real identity is always the
composite key `(agent, user, sessionId)` — see
[docs/sessions.md](./sessions.md).

| Subject | Request | Reply |
|---|---|---|
| `botson.sessions.list` | `{"agent"?, "user"?}` — both optional filters; omit both to list everything | `[{"id","agentName","userId","displayName","lastUpdateTime","eventCount"}, ...]`, most-recently-updated first |
| `botson.sessions.get` | `{"agent","user","sessionId"}` — all required | `{...SessionStat fields, "state":{...}, "events":[{"author","timestamp","text"}, ...]}` |
| `botson.sessions.delete` | `{"agent","user","sessionId"}` — all required | `{}` on success |

### Dashboard aggregation

| Subject | Request | Reply |
|---|---|---|
| `botson.dashboard.stats` | *(empty)* | `{"totalAgents","totalSessions","totalEvents","dbPath","agents":[{"name","description","isRoot","sessionCount"}, ...],"recentSessions":[...up to 10 SessionStat...]}` |
| `botson.dashboard.users` | *(empty)* | `["user-id-1","user-id-2",...]` — every distinct user ID actually present in session data, sorted. Empty if no sessions exist yet. **No default/seed value** — see §5. |

---

## 5. There is no default "user"

The core makes no assumptions about what a user ID looks like or what
value it should default to. Every subject that's scoped to a session
requires the caller to supply `user` (or `userId` on `adk.*`) explicitly —
there's no fallback like the old bundled TUI's hardcoded `"tui"` or the old
web console's `"web"`.

This means **you choose your own user-ID scheme.** A few reasonable
patterns:
- One fixed ID per consumer app (`"my-web-console"`) if you don't need
  per-person session isolation.
- One ID per real end-user (`"discord:123456789"`, `"web:<account-id>"`)
  if you do.
- Something else entirely — the core just stores and filters on whatever
  string you send.

Whatever you pick, use it consistently: session lookups
(`botson.sessions.get`, `adk.rest.GET` on a session path) require the
*exact* `(agent, user, sessionId)` triple a session was created under.

---

## See also

- [docs/process-architecture.md](./process-architecture.md) — the core process itself: lifecycle, discovery, workspace resolution.
- [docs/sessions.md](./sessions.md) — session data model and storage schema.
- [AGENTS.md](../AGENTS.md) — full project reference, including the HITL wire protocol and tool registry.
- [NATS-ADK-Proxy](https://github.com/Savs-Agents/NATS-ADK-Proxy) — the imported package implementing `adk.*`; its README and `protocol` package godoc are the authoritative source for that namespace.
