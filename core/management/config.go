package management

import (
	"fmt"

	"botsonv2/core/config"
)

const maskedSecret = "******"

// GetMaskedConfig loads the application config and masks secret fields
// (Gemini API key, Discord token) so it's safe to hand to a UI.
func GetMaskedConfig() (*config.AppConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	masked := *cfg
	if masked.GeminiAPIKey != "" {
		masked.GeminiAPIKey = maskedSecret
	}
	if masked.Discord.Token != "" {
		masked.Discord.Token = maskedSecret
	}
	return &masked, nil
}

// UpdateConfig merges a (possibly secret-masked) config update against the
// on-disk config and persists it. If the Discord token changed while the
// background gateway is running, the caller needs to restart it (via
// StopDiscordDaemon/StartDiscordDaemon) for the change to take effect.
func UpdateConfig(req *config.AppConfig) error {
	diskCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load existing config: %w", err)
	}

	// Retain existing secrets if the caller sent back the masked placeholder
	if req.GeminiAPIKey == maskedSecret {
		req.GeminiAPIKey = diskCfg.GeminiAPIKey
	}
	if req.Discord.Token == maskedSecret {
		req.Discord.Token = diskCfg.Discord.Token
	}

	if err := config.Save(req); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}
