package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"time"

	"botson/internal/config"

	"google.golang.org/adk/v2/model"
)

const openRouterEndpoint = "https://openrouter.ai/api/v1/chat/completions"

// openRouterModel implements model.LLM against OpenRouter's
// OpenAI-chat-completions-compatible API. Unlike Gemini's own protocol,
// this can't reuse genai.Client the way model/apigee does for a
// Gemini-compatible proxy, so requests/responses are translated through
// openrouter_convert.go.
type openRouterModel struct {
	httpClient *http.Client
	apiKey     string
	modelName  string
}

func newOpenRouterModel(cfg *config.AppConfig) (model.LLM, error) {
	if cfg.OpenRouterAPIKey == "" {
		return nil, fmt.Errorf("openrouter provider selected but no OpenRouter API key is configured")
	}
	return &openRouterModel{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		apiKey:     cfg.OpenRouterAPIKey,
		modelName:  cfg.ModelName,
	}, nil
}

func (m *openRouterModel) Name() string {
	return m.modelName
}

// GenerateContent always performs a single blocking HTTP call and yields
// one response, regardless of the stream flag -- Botson's NATS
// request/reply transport never requests SSE streaming today (see
// internal/natsapi's package doc: "none of it needs streaming"), so
// there's nothing to stream to. If stream=true is ever requested this
// still behaves correctly, just as one non-partial chunk instead of
// several partial ones.
func (m *openRouterModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

func (m *openRouterModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	body, err := json.Marshal(toChatRequest(m.modelName, req))
	if err != nil {
		return nil, fmt.Errorf("failed to encode OpenRouter request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenRouter request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/xSaVageAU/Botson-ADKv2")
	httpReq.Header.Set("X-Title", "Botson")

	httpResp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call OpenRouter: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OpenRouter response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenRouter request failed (%d): %s", httpResp.StatusCode, describeOpenRouterError(respBody))
	}

	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to parse OpenRouter response: %w", err)
	}
	if chatResp.Error != nil {
		return nil, fmt.Errorf("OpenRouter error: %s", chatResp.Error.describe())
	}

	return fromChatResponse(&chatResp)
}

// describeOpenRouterError best-effort extracts a human-readable message
// from a non-2xx OpenRouter response, falling back to the raw (truncated)
// body if it isn't the expected {"error":{"message":...}} shape.
func describeOpenRouterError(body []byte) string {
	var withErr struct {
		Error *chatAPIError `json:"error"`
	}
	if err := json.Unmarshal(body, &withErr); err == nil && withErr.Error != nil && withErr.Error.Message != "" {
		return withErr.Error.describe()
	}
	if len(body) > 500 {
		body = body[:500]
	}
	return string(body)
}
