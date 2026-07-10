// Package natsapi is the server side of Botson's own NATS API: the
// subjects covering everything about running Botson that isn't part of
// stock ADK's REST/A2A surface -- that surface is fronted separately by an
// imported github.com/Savs-Agents/NATS-ADK-Proxy under the "adk." subject
// prefix (see cmd/botson-core/cmd_core.go). This package's subjects, all under
// "botson.", cover settings, custom-agent CRUD, and session/dashboard
// management -- the things every CLI subcommand used to do by touching
// config.json/the session DB/~/.botson/agents directly. Every subject
// here is plain request/reply; none of it needs streaming.
package natsapi

import "botson/internal/config"

const (
	SubjectSettingsGet = "botson.settings.get"
	SubjectSettingsSet = "botson.settings.set"

	SubjectAgentsList   = "botson.agents.list"
	SubjectAgentsTools  = "botson.agents.tools"
	SubjectAgentsSave   = "botson.agents.save"
	SubjectAgentsDelete = "botson.agents.delete"

	SubjectSessionsList        = "botson.sessions.list"
	SubjectSessionsGet         = "botson.sessions.get"
	SubjectSessionsDelete      = "botson.sessions.delete"
	SubjectSessionsSetAutoMode = "botson.sessions.setAutoMode"

	SubjectDashboardStats = "botson.dashboard.stats"
	SubjectDashboardUsers = "botson.dashboard.users"
)

// SettingsSetRequest changes only the fields that are non-nil, mirroring
// `settings set`'s old "only touch the flags you actually pass" semantics
// (cmd.Flags().Changed) now expressed on the wire as optional pointers.
type SettingsSetRequest struct {
	ModelName        *string `json:"modelName,omitempty"`
	RootAgent        *string `json:"rootAgent,omitempty"`
	GeminiAPIKey     *string `json:"geminiApiKey,omitempty"`
	WorkspaceRoot    *string `json:"workspaceRoot,omitempty"`
	Provider         *string `json:"provider,omitempty"`
	OpenRouterAPIKey *string `json:"openRouterApiKey,omitempty"`
}

// SettingsSetReply is settings.set's reply: the same fields settings.get
// returns (embedded, so they marshal at the top level), plus an optional
// Note -- set when modelName/provider changed, since this already-running
// core process won't actually use the new model until it's restarted (see
// internal/tools/update_settings.go's UpdateSettings, which carries the
// same warning for the agent-editable path).
type SettingsSetReply struct {
	config.AppConfig
	Note string `json:"note,omitempty"`
}

// AgentsSaveRequest is the request payload for SubjectAgentsSave.
type AgentsSaveRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tools        []string `json:"tools"`
	Private      bool     `json:"private"`
	Instructions string   `json:"instructions"`
}

// AgentsDeleteRequest is the request payload for SubjectAgentsDelete.
type AgentsDeleteRequest struct {
	Name string `json:"name"`
}

// SessionsListRequest is the request payload for SubjectSessionsList.
type SessionsListRequest struct {
	Agent string `json:"agent,omitempty"`
	User  string `json:"user,omitempty"`
}

// SessionsGetRequest and SessionsDeleteRequest identify one session by its
// full composite key -- (AppName, UserID, SessionID) is the actual identity,
// not the session ID alone.
type SessionsGetRequest struct {
	Agent     string `json:"agent"`
	User      string `json:"user"`
	SessionID string `json:"sessionId"`
}

type SessionsDeleteRequest struct {
	Agent     string `json:"agent"`
	User      string `json:"user"`
	SessionID string `json:"sessionId"`
}

// SessionsSetAutoModeRequest is the request payload for
// SubjectSessionsSetAutoMode -- see management.AutoModeStateKey.
type SessionsSetAutoModeRequest struct {
	Agent     string `json:"agent"`
	User      string `json:"user"`
	SessionID string `json:"sessionId"`
	Enabled   bool   `json:"enabled"`
}
