package providers

import (
	"encoding/json"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestToChatRequest_TextRoundTrip(t *testing.T) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "hi there"}}},
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "be nice"}}},
		},
	}

	out := toChatRequest("some-model", req)

	if out.Model != "some-model" {
		t.Fatalf("Model = %q, want %q", out.Model, "some-model")
	}
	if len(out.Messages) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(out.Messages), out.Messages)
	}
	wantMsgs := []chatMessage{
		{Role: "system", Content: "be nice"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	for i, want := range wantMsgs {
		got := out.Messages[i]
		if got.Role != want.Role || got.Content != want.Content || len(got.ToolCalls) != 0 {
			t.Errorf("Messages[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestContentToMessages_FunctionCall(t *testing.T) {
	c := &genai.Content{
		Role: "model",
		Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{
				ID:   "call_abc",
				Name: "listFiles",
				Args: map[string]any{"subdir": "foo"},
			}},
		},
	}

	msgs := contentToMessages(c)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(msgs), msgs)
	}
	msg := msgs[0]
	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", msg.Role)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Function.Name != "listFiles" {
		t.Errorf("tool call = %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if args["subdir"] != "foo" {
		t.Errorf("args = %+v", args)
	}
}

func TestContentToMessages_FunctionResponse(t *testing.T) {
	c := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				ID:       "call_abc",
				Name:     "listFiles",
				Response: map[string]any{"output": "a.txt"},
			}},
		},
	}

	msgs := contentToMessages(c)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(msgs), msgs)
	}
	msg := msgs[0]
	if msg.Role != "tool" || msg.ToolCallID != "call_abc" {
		t.Errorf("message = %+v", msg)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(msg.Content), &resp); err != nil {
		t.Fatalf("content not valid JSON: %v", err)
	}
	if resp["output"] != "a.txt" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestFromChatResponse_MultiToolCall(t *testing.T) {
	resp := &chatCompletionResponse{
		Model: "some-model",
		Choices: []chatChoice{{
			FinishReason: "tool_calls",
			Message: chatResponseMessage{
				Role:    "assistant",
				Content: "",
				ToolCalls: []chatToolCall{
					{ID: "call_1", Type: "function", Function: chatToolCallFunc{Name: "readFile", Arguments: `{"path":"a.txt"}`}},
					{ID: "call_2", Type: "function", Function: chatToolCallFunc{Name: "readFile", Arguments: `{"path":"b.txt"}`}},
				},
			},
		}},
		Usage: &chatUsage{PromptTokens: 10, CompletionTokens: 5},
	}

	out, err := fromChatResponse(resp)
	if err != nil {
		t.Fatalf("fromChatResponse: %v", err)
	}
	if out.Content == nil || len(out.Content.Parts) != 2 {
		t.Fatalf("got parts %+v", out.Content)
	}
	fc1 := out.Content.Parts[0].FunctionCall
	fc2 := out.Content.Parts[1].FunctionCall
	if fc1 == nil || fc1.ID != "call_1" || fc1.Args["path"] != "a.txt" {
		t.Errorf("fc1 = %+v", fc1)
	}
	if fc2 == nil || fc2.ID != "call_2" || fc2.Args["path"] != "b.txt" {
		t.Errorf("fc2 = %+v", fc2)
	}
	if out.FinishReason != genai.FinishReasonStop {
		t.Errorf("FinishReason = %v, want Stop", out.FinishReason)
	}
	if out.UsageMetadata == nil || out.UsageMetadata.PromptTokenCount != 10 || out.UsageMetadata.CandidatesTokenCount != 5 {
		t.Errorf("UsageMetadata = %+v", out.UsageMetadata)
	}
}

func TestSchemaToJSONSchema_NestedObjectAndArray(t *testing.T) {
	s := &genai.Schema{
		Type: genai.Type("OBJECT"),
		Properties: map[string]*genai.Schema{
			"tags": {
				Type:  genai.Type("ARRAY"),
				Items: &genai.Schema{Type: genai.Type("STRING")},
			},
			"count": {Type: genai.Type("INTEGER")},
		},
		Required: []string{"tags"},
	}

	out := schemaToJSONSchema(s)
	if out["type"] != "object" {
		t.Errorf("type = %v", out["type"])
	}
	props, ok := out["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties not a map: %+v", out["properties"])
	}
	tags, ok := props["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags not a map: %+v", props["tags"])
	}
	if tags["type"] != "array" {
		t.Errorf("tags.type = %v", tags["type"])
	}
	items, ok := tags["items"].(map[string]any)
	if !ok || items["type"] != "string" {
		t.Errorf("tags.items = %+v", tags["items"])
	}
	count, ok := props["count"].(map[string]any)
	if !ok || count["type"] != "integer" {
		t.Errorf("count = %+v", props["count"])
	}
	reqList, ok := out["required"].([]string)
	if !ok || len(reqList) != 1 || reqList[0] != "tags" {
		t.Errorf("required = %+v", out["required"])
	}
}

func TestMapFinishReason(t *testing.T) {
	cases := map[string]genai.FinishReason{
		"stop":           genai.FinishReasonStop,
		"tool_calls":     genai.FinishReasonStop,
		"":               genai.FinishReasonStop,
		"length":         genai.FinishReasonMaxTokens,
		"content_filter": genai.FinishReasonSafety,
		"something_else": genai.FinishReasonOther,
	}
	for in, want := range cases {
		if got := mapFinishReason(in); got != want {
			t.Errorf("mapFinishReason(%q) = %v, want %v", in, got, want)
		}
	}
}
