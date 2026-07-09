package tui

import (
	"fmt"

	"botsonv2/internal/interface/apiclient"

	"github.com/charmbracelet/lipgloss"
)

// replayHistory turns a resumed session's stored events into the same
// styled transcript lines Update appends to live, so reattaching to a
// session looks like the conversation never stopped. It deliberately
// mirrors only what the live TUI already shows -- text turns and a
// one-line note per tool call, no tool results -- rather than the web
// console's fuller trace view (see AGENTS.md's CLI/TUI-vs-webui feature
// comparison); the adk_request_confirmation wrapper call/response pair
// (see AGENTS.md's "HITL confirmation wire protocol") is internal
// plumbing for a decision already made, so it's skipped entirely rather
// than re-rendering a stale approve/deny prompt.
func replayHistory(events []apiclient.Event, agentName string, userStyle, agentStyle, toolStyle lipgloss.Style) []string {
	var history []string
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			switch {
			case part.Text != "" && ev.Author == "user":
				history = append(history, userStyle.Render("[User] > ")+part.Text)
			case part.Text != "":
				history = append(history, agentStyle.Render(fmt.Sprintf("[%s] > ", agentName))+part.Text)
			case part.FunctionCall != nil && part.FunctionCall.Name != confirmationToolName:
				history = append(history, toolStyle.Render(fmt.Sprintf("⚙️ tool: %s", part.FunctionCall.Name)))
			}
		}
	}
	return history
}
