package apiclient

import "google.golang.org/genai"

// Event mirrors the subset of ADK's REST wire-format event this client
// actually needs -- author and content -- reusing genai's own public
// types for Content/Part/FunctionCall/FunctionResponse so no field
// mapping is needed for the parts Botson cares about. ADK's own
// equivalent struct lives in its unexported server/adkrest/internal/models
// package, so this local mirror is the actual fix for that, not a
// workaround to revisit later.
type Event struct {
	Author  string         `json:"author"`
	Content *genai.Content `json:"content"`
}
