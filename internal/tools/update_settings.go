package tools

import (
	"fmt"

	"botson/internal/config"

	"google.golang.org/adk/v2/agent"
)

// UpdateSettingsArgs defines the input arguments for the Update Settings
// tool. Every field is optional; only the ones set (non-empty) are
// changed, everything else is left as-is. Deliberately excludes secrets
// (the Gemini/OpenRouter API keys) -- those stay human-controlled via
// `botson settings set`, not agent-editable.
type UpdateSettingsArgs struct {
	ModelName string `json:"modelName,omitempty" jsonschema:"The model to use -- a bare Gemini model name (e.g. 'gemini-3.1-flash-lite') or, when provider is 'openrouter', a full OpenRouter model slug (e.g. 'anthropic/claude-3.5-sonnet'). Leave empty to keep the current model."`
	RootAgent string `json:"rootAgent,omitempty" jsonschema:"Name of the agent that runs by default. Leave empty to keep the current root agent."`
	Provider  string `json:"provider,omitempty" jsonschema:"Which LLM backend to use: 'gemini' or 'openrouter'. Leave empty to keep the current provider. Takes effect on the next core restart, same as a model change."`
}

// UpdateSettingsResult echoes back the resulting configuration (secrets
// masked) so the agent can confirm what actually changed. Note is set when
// something was changed that this already-running process won't actually
// pick up until it's restarted.
type UpdateSettingsResult struct {
	Updated config.AppConfig `json:"updated"`
	Note    string           `json:"note,omitempty"`
}

// UpdateSettings lets the running agent change its own non-secret settings
// mid-conversation. The change is written to disk immediately via
// config.Update, which also mutates the shared in-memory config this
// process is already holding (e.g. cmd/botson-core's appBoot.Config).
// RootAgent takes effect for the rest of this run. ModelName and Provider
// do NOT -- the model.LLM this process talks to was already built once at
// boot (see cmd/botson-core/bootstrap.go, internal/providers.New) and
// isn't rebuilt on a settings change, so this process keeps using whatever
// provider/model it started with until it's restarted, even though this
// reply (and botson.settings.get) will show the new value.
func UpdateSettings(ctx agent.Context, args UpdateSettingsArgs) (UpdateSettingsResult, error) {
	modelOrProviderChanged := args.ModelName != "" || args.Provider != ""

	cfg, err := config.Update(func(cfg *config.AppConfig) {
		if args.ModelName != "" {
			cfg.ModelName = args.ModelName
		}
		if args.RootAgent != "" {
			cfg.RootAgent = args.RootAgent
		}
		if args.Provider != "" {
			cfg.Provider = args.Provider
		}
	})
	if err != nil {
		return UpdateSettingsResult{}, fmt.Errorf("failed to update settings: %w", err)
	}

	result := UpdateSettingsResult{Updated: config.Mask(cfg)}
	if modelOrProviderChanged {
		result.Note = "modelName/provider are saved, but this running core process is still using the model it booted with -- restart the core (`botson core stop` then start it again) for this change to actually take effect."
	}
	return result, nil
}
