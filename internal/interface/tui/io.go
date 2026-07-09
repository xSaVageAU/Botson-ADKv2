package tui

import (
	"context"

	"google.golang.org/genai"
)

// confirmationToolName is the synthetic function-call name ADK uses to
// signal a confirmation-gated tool call is waiting for a yes/no answer --
// see this project's AGENTS.md "HITL confirmation wire protocol" section.
const confirmationToolName = "adk_request_confirmation"

func (m model) runAgentStream(text string) {
	m.streamEvents(context.Background(), &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: text}},
	})
}

// resumeAfterConfirmation answers a pending adk_request_confirmation call
// and streams whatever the agent does next -- the
// FunctionResponse{Name: "adk_request_confirmation", ID, Response:
// {"confirmed": bool}} shape ADK itself expects any caller to reply with.
func (m model) resumeAfterConfirmation(callID string, approved bool) {
	m.streamEvents(context.Background(), &genai.Content{
		Role: "user",
		Parts: []*genai.Part{{
			FunctionResponse: &genai.FunctionResponse{
				Name:     confirmationToolName,
				ID:       callID,
				Response: map[string]any{"confirmed": approved},
			},
		}},
	})
}

// streamEvents drives one call to the core's /api/run_sse (via m.client)
// and forwards each event to the Bubble Tea program as it arrives. Shared
// by the initial send and by resuming after a HITL confirmation, since
// both are just a POST with a different message content.
func (m model) streamEvents(ctx context.Context, msg *genai.Content) {
	runIter := m.client.Run(ctx, m.agentName, "tui", m.sessionID, msg)

	for event, err := range runIter {
		if err != nil {
			program.Send(responseErrMsg{err: err})
			return
		}
		if event == nil || event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.Text != "" {
				program.Send(responseChunkMsg(part.Text))
			}
			if part.FunctionCall != nil {
				if part.FunctionCall.Name == confirmationToolName {
					callID, toolName, hint := extractConfirmationRequest(part.FunctionCall)
					program.Send(hitlPendingMsg{callID: callID, toolName: toolName, hint: hint})
				} else {
					program.Send(toolCallMsg(part.FunctionCall.Name))
				}
			}
		}
	}
	program.Send(responseDoneMsg{})
}

// extractConfirmationRequest reads the call id to answer, the original
// tool's name, and the human-readable hint out of an adk_request_
// confirmation call's Args -- a generic map[string]any (this arrives via
// JSON, not a typed struct), so accessed by key rather than field. Note
// the hint lives nested under args.toolConfirmation.hint, not a top-level
// args.hint -- verified against a real captured wire payload; the web
// console's chat.js currently reads the (always-empty) top-level field
// instead, a pre-existing latent bug there worth fixing separately.
func extractConfirmationRequest(fc *genai.FunctionCall) (callID, toolName, hint string) {
	callID = fc.ID
	toolName = "tool"

	if orig, ok := fc.Args["originalFunctionCall"].(map[string]any); ok {
		if name, ok := orig["name"].(string); ok && name != "" {
			toolName = name
		}
	}
	if tc, ok := fc.Args["toolConfirmation"].(map[string]any); ok {
		if h, ok := tc["hint"].(string); ok {
			hint = h
		}
	}
	return callID, toolName, hint
}
