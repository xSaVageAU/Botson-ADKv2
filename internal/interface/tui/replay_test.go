package tui

import (
	"strings"
	"testing"

	"botsonv2/internal/interface/apiclient"

	"github.com/charmbracelet/lipgloss"
	"google.golang.org/genai"
)

func TestReplayHistoryRendersTextTurnsAndToolCalls(t *testing.T) {
	events := []apiclient.Event{
		{Author: "user", Content: &genai.Content{Parts: []*genai.Part{{Text: "hi there"}}}},
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "call-1", Name: "writeFile"}},
		}}},
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{{Text: "done"}}}},
	}

	history := replayHistory(events, "Agent Botson", lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())

	if len(history) != 3 {
		t.Fatalf("expected 3 history lines, got %d: %v", len(history), history)
	}
	if !strings.Contains(history[0], "hi there") {
		t.Errorf("expected user text in first line, got %q", history[0])
	}
	if !strings.Contains(history[1], "writeFile") {
		t.Errorf("expected tool call name in second line, got %q", history[1])
	}
	if !strings.Contains(history[2], "done") {
		t.Errorf("expected agent text in third line, got %q", history[2])
	}
}

func TestReplayHistorySkipsConfirmationPlumbing(t *testing.T) {
	events := []apiclient.Event{
		{Author: "Agent Botson", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "call-2", Name: confirmationToolName}},
		}}},
		{Author: "user", Content: &genai.Content{Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{Name: confirmationToolName, ID: "call-2", Response: map[string]any{"confirmed": true}}},
		}}},
	}

	history := replayHistory(events, "Agent Botson", lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())

	if len(history) != 0 {
		t.Fatalf("expected the confirmation request/response pair to be skipped entirely, got: %v", history)
	}
}
