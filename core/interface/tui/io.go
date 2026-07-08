package tui

import (
	"context"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/genai"
)

func (m model) runAgentStream(text string) {
	ctx := context.Background()
	userMsg := genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: text}},
	}
	runIter := m.runner.Run(ctx, "tui", m.sessionID, &userMsg, adkagent.RunConfig{})

	for event, err := range runIter {
		if err != nil {
			program.Send(responseErrMsg{err: err})
			return
		}
		if event == nil {
			continue
		}
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					program.Send(responseChunkMsg(part.Text))
				}
				if part.FunctionCall != nil {
					program.Send(toolCallMsg(part.FunctionCall.Name))
				}
			}
		}
	}
	program.Send(responseDoneMsg{})
}
