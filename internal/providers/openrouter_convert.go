package providers

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// --- OpenAI-chat-completions-shaped wire types (OpenRouter's request/response) ---

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	Temperature *float32      `json:"temperature,omitempty"`
	TopP        *float32      `json:"top_p,omitempty"`
	MaxTokens   int32         `json:"max_tokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatCompletionResponse struct {
	Model   string        `json:"model"`
	Choices []chatChoice  `json:"choices"`
	Usage   *chatUsage    `json:"usage"`
	Error   *chatAPIError `json:"error"`
}

type chatChoice struct {
	Message      chatResponseMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type chatResponseMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls"`
}

type chatUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
}

type chatAPIError struct {
	Message string `json:"message"`
}

// --- request translation: model.LLMRequest (genai-shaped) -> chatCompletionRequest ---

func toChatRequest(modelName string, req *model.LLMRequest) *chatCompletionRequest {
	out := &chatCompletionRequest{Model: modelName}

	if req.Config != nil && req.Config.SystemInstruction != nil {
		if text := joinText(req.Config.SystemInstruction.Parts); text != "" {
			out.Messages = append(out.Messages, chatMessage{Role: "system", Content: text})
		}
	}

	for _, c := range req.Contents {
		out.Messages = append(out.Messages, contentToMessages(c)...)
	}

	if req.Config != nil {
		out.Tools = toTools(req.Config.Tools)
		out.Temperature = req.Config.Temperature
		out.TopP = req.Config.TopP
		out.MaxTokens = req.Config.MaxOutputTokens
		out.Stop = req.Config.StopSequences
	}

	return out
}

func joinText(parts []*genai.Part) string {
	var texts []string
	for _, p := range parts {
		if p != nil && p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// contentToMessages converts one genai.Content into zero or more OpenAI
// chat messages. A Content's parts are either function responses (tool
// results -- always role "user" in ADK's wire convention, see
// Botson-TUI's chat.go answerConfirm) or a mix of text/function calls
// (role "user" or "model"); never both kinds in the same Content, so the
// function-response branch below is checked first and returns early.
func contentToMessages(c *genai.Content) []chatMessage {
	if c == nil {
		return nil
	}

	var toolMsgs []chatMessage
	for i, p := range c.Parts {
		if p == nil || p.FunctionResponse == nil {
			continue
		}
		fr := p.FunctionResponse
		id := fr.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		toolMsgs = append(toolMsgs, chatMessage{
			Role:       "tool",
			ToolCallID: id,
			Content:    marshalOrEmpty(fr.Response),
		})
	}
	if len(toolMsgs) > 0 {
		return toolMsgs
	}

	var textParts []string
	var toolCalls []chatToolCall
	for i, p := range c.Parts {
		if p == nil {
			continue
		}
		if p.Text != "" {
			textParts = append(textParts, p.Text)
		}
		if p.FunctionCall != nil {
			fc := p.FunctionCall
			id := fc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			args, _ := json.Marshal(fc.Args)
			toolCalls = append(toolCalls, chatToolCall{
				ID:       id,
				Type:     "function",
				Function: chatToolCallFunc{Name: fc.Name, Arguments: string(args)},
			})
		}
	}
	if len(textParts) == 0 && len(toolCalls) == 0 {
		return nil
	}

	role := "user"
	if c.Role == "model" {
		role = "assistant"
	}
	return []chatMessage{{
		Role:      role,
		Content:   strings.Join(textParts, "\n"),
		ToolCalls: toolCalls,
	}}
}

func marshalOrEmpty(v map[string]any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// toTools flattens every FunctionDeclaration across all of req.Config.Tools
// into OpenAI's flat "tools" array shape.
func toTools(tools []*genai.Tool) []chatTool {
	var out []chatTool
	for _, t := range tools {
		if t == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			out = append(out, chatTool{
				Type: "function",
				Function: chatToolFunction{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  schemaToJSONSchema(fd.Parameters),
				},
			})
		}
	}
	return out
}

// schemaToJSONSchema converts a genai.Schema (Gemini's OpenAPI-3.0-flavored
// schema, e.g. Type "OBJECT"/"STRING") into a plain JSON Schema map (e.g.
// type "object"/"string") as OpenAI-compatible tool definitions expect.
func schemaToJSONSchema(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	out := map[string]any{}
	if s.Type != "" {
		out["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if s.Items != nil {
		out["items"] = schemaToJSONSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			props[name] = schemaToJSONSchema(prop)
		}
		out["properties"] = props
	}
	return out
}

// --- response translation: chatCompletionResponse -> model.LLMResponse (genai-shaped) ---

func fromChatResponse(resp *chatCompletionResponse) (*model.LLMResponse, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	choice := resp.Choices[0]

	var parts []*genai.Part
	if choice.Message.Content != "" {
		parts = append(parts, &genai.Part{Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("failed to parse tool call arguments for %q: %w", tc.Function.Name, err)
			}
		}
		parts = append(parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	out := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		FinishReason: mapFinishReason(choice.FinishReason),
		ModelVersion: resp.Model,
	}
	if resp.Usage != nil {
		out.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     resp.Usage.PromptTokens,
			CandidatesTokenCount: resp.Usage.CompletionTokens,
		}
	}
	return out, nil
}

// mapFinishReason maps OpenAI/OpenRouter's finish_reason strings to
// genai.FinishReason. "tool_calls" maps to Stop (not a distinct genai
// value) because ADK's tool-continuation loop keys off the presence of
// FunctionCall parts in the response content, not FinishReason -- see
// internal/llminternal/converters.Genai2LLMResponse in the ADK module,
// which only special-cases FinishReasonStop itself.
func mapFinishReason(reason string) genai.FinishReason {
	switch reason {
	case "stop", "tool_calls", "":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "content_filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonOther
	}
}
