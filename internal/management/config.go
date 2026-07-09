package management

import (
	"fmt"

	"botson/internal/config"
)

// GetMaskedConfig loads the application config and masks secret fields
// (the Gemini API key) so it's safe to hand to a UI.
func GetMaskedConfig() (*config.AppConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	masked := config.Mask(cfg)
	return &masked, nil
}
