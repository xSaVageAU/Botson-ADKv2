package management

import (
	"fmt"

	"botsonv2/core/config"
)

// GetMaskedConfig loads the application config and masks secret fields
// (Gemini API key, Discord token) so it's safe to hand to a UI.
func GetMaskedConfig() (*config.AppConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	masked := config.Mask(cfg)
	return &masked, nil
}

// UpdateConfig merges a (possibly secret-masked) config update against the
// current config and persists it in place, so the running process's own
// copy (e.g. cmd/botson's appBoot.Config) reflects the change immediately.
// If the Discord token changed while the background gateway is running,
// the caller needs to restart it (via StopDiscordDaemon/StartDiscordDaemon)
// for the change to take effect there.
func UpdateConfig(req *config.AppConfig) error {
	_, err := config.Update(func(cfg *config.AppConfig) {
		// Retain existing secrets if the caller sent back the masked placeholder
		geminiKey := req.GeminiAPIKey
		if geminiKey == config.MaskedSecret {
			geminiKey = cfg.GeminiAPIKey
		}
		discordToken := req.Discord.Token
		if discordToken == config.MaskedSecret {
			discordToken = cfg.Discord.Token
		}

		*cfg = *req
		cfg.GeminiAPIKey = geminiKey
		cfg.Discord.Token = discordToken
	})
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}
