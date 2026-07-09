package tools

import (
	"fmt"

	"botsonv2/internal/config"

	"google.golang.org/adk/v2/agent"
)

// UpdateSettingsArgs defines the input arguments for the Update Settings
// tool. Every field is optional; only the ones set (non-empty) are
// changed, everything else is left as-is. Deliberately excludes secrets
// (the Gemini API key) -- those stay human-controlled via `botson settings
// set`, not agent-editable.
type UpdateSettingsArgs struct {
	ModelName string `json:"modelName,omitempty" jsonschema:"The Gemini model to use (e.g. 'gemini-3.1-flash-lite'). Leave empty to keep the current model."`
	RootAgent string `json:"rootAgent,omitempty" jsonschema:"Name of the agent that runs by default. Leave empty to keep the current root agent."`
}

// UpdateSettingsResult echoes back the resulting configuration (secrets
// masked) so the agent can confirm what actually changed.
type UpdateSettingsResult struct {
	Updated config.AppConfig `json:"updated"`
}

// UpdateSettings lets the running agent change its own non-secret settings
// mid-conversation. The change is written to disk immediately via
// config.Update, which also mutates the shared in-memory config this
// process is already holding (e.g. cmd/botson's appBoot.Config), so it
// takes effect for the rest of this run without needing a restart.
func UpdateSettings(ctx agent.Context, args UpdateSettingsArgs) (UpdateSettingsResult, error) {
	cfg, err := config.Update(func(cfg *config.AppConfig) {
		if args.ModelName != "" {
			cfg.ModelName = args.ModelName
		}
		if args.RootAgent != "" {
			cfg.RootAgent = args.RootAgent
		}
	})
	if err != nil {
		return UpdateSettingsResult{}, fmt.Errorf("failed to update settings: %w", err)
	}

	return UpdateSettingsResult{Updated: config.Mask(cfg)}, nil
}
