// Package natscore is the server side of Botson's core NATS API: the one
// wire protocol any process (this repo's TUI, and future standalone
// Discord/web projects) uses to talk to a running core, replacing the HTTP
// REST/SSE surface ADK's own launcher stack used to provide. Serve wraps
// the same in-process runner.Runner/session.Service/agent.Loader calls
// internal/interface/discord used to make directly (before that package was
// removed in favor of this), just publishing to NATS instead of posting to
// Discord.
package natscore

import "google.golang.org/genai"

// Subject names for Botson's core NATS API, all under one "botson."
// prefix so a shared NATS server/cluster can host other unrelated
// subjects without collision.
const (
	SubjectDefaultAgent  = "botson.agent.default"
	SubjectSessionCreate = "botson.session.create"
	SubjectSessionGet    = "botson.session.get"
	SubjectRun           = "botson.run"
)

// Event mirrors the subset of an ADK session.Event a caller actually needs
// -- author and content -- reusing genai's own public types for
// Content/Part/FunctionCall/FunctionResponse.
type Event struct {
	Author  string         `json:"author"`
	Content *genai.Content `json:"content"`
}

// SessionInfo carries a session's stored events (to replay into a fresh
// transcript) and state (which carries e.g. __session_metadata__).
type SessionInfo struct {
	Events []Event        `json:"events"`
	State  map[string]any `json:"state"`
}

// Frame is one message published to a botson.run caller's reply inbox.
// Exactly one of Event, Error, or Done is meaningful per frame: zero or
// more Event frames, then exactly one final frame that is either Error or
// Done. There is no NATS-level "end of stream" signal the way an HTTP
// response body ending is implicit, so Done is the explicit terminator a
// caller reads until.
type Frame struct {
	Event *Event `json:"event,omitempty"`
	Error string `json:"error,omitempty"`
	Done  bool   `json:"done,omitempty"`
}

// DefaultAgentReply answers SubjectDefaultAgent.
type DefaultAgentReply struct {
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
}

// SessionCreateRequest is the request payload for SubjectSessionCreate. If
// SessionID is empty, the core generates one.
type SessionCreateRequest struct {
	AppName   string         `json:"appName"`
	UserID    string         `json:"userId"`
	SessionID string         `json:"sessionId"`
	State     map[string]any `json:"state"`
}

// SessionCreateReply answers SubjectSessionCreate.
type SessionCreateReply struct {
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

// SessionGetRequest is the request payload for SubjectSessionGet.
type SessionGetRequest struct {
	AppName   string `json:"appName"`
	UserID    string `json:"userId"`
	SessionID string `json:"sessionId"`
}

// SessionGetReply answers SubjectSessionGet.
type SessionGetReply struct {
	SessionInfo
	Error string `json:"error,omitempty"`
}

// RunRequest is the request payload published to SubjectRun; the reply
// inbox (NATS message Reply field) is where the resulting Frames are
// published.
type RunRequest struct {
	AppName    string         `json:"appName"`
	UserID     string         `json:"userId"`
	SessionID  string         `json:"sessionId"`
	NewMessage *genai.Content `json:"newMessage"`
}
