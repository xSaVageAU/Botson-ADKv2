// Package providers builds the google.golang.org/adk/v2/model.LLM Botson's
// core runs on, based on config.AppConfig.Provider. model.LLM is a small
// interface (Name, GenerateContent), so any backend -- Gemini's own wire
// protocol or an OpenAI-chat-completions-shaped one like OpenRouter -- can
// be dropped in behind it without touching agent construction.
package providers

import (
	"context"
	"fmt"

	"botson/internal/config"

	"google.golang.org/adk/v2/model"
)

// New builds the model.LLM for cfg.Provider ("gemini" or "openrouter";
// empty is treated as "gemini" for configs written before this field
// existed).
func New(ctx context.Context, cfg *config.AppConfig) (model.LLM, error) {
	switch cfg.Provider {
	case "", "gemini":
		return newGeminiModel(ctx, cfg)
	case "openrouter":
		return newOpenRouterModel(cfg)
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
