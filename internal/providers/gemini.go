package providers

import (
	"context"
	"fmt"

	"botson/internal/config"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// newGeminiModel builds the Gemini-backed model.LLM, unchanged from how
// cmd/botson-core/bootstrap.go constructed it before this package existed.
func newGeminiModel(ctx context.Context, cfg *config.AppConfig) (model.LLM, error) {
	m, err := gemini.NewModel(ctx, cfg.ModelName, &genai.ClientConfig{
		APIKey: cfg.GeminiAPIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
	}
	return m, nil
}
