package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppConfig holds the application configuration.
type AppConfig struct {
	ModelName    string        `json:"model_name"`
	GeminiAPIKey string        `json:"gemini_api_key"`
	RootAgent    string        `json:"root_agent"`
	Discord      DiscordConfig `json:"discord"`
}

// DiscordConfig holds parameters for the Discord gateway integration.
type DiscordConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token"`
	OwnerID   string   `json:"owner_id"`
	Whitelist []string `json:"whitelist"`
}

// GetConfigPath returns the absolute path to ~/.botsonv2/config.json
func GetConfigPath() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "config.json"), nil
}

// Load loads the configuration from ~/.botsonv2/config.json.
// If the file does not exist, it returns a default initialized configuration template.
func Load() (*AppConfig, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return a default configuration template
			defaultCfg := &AppConfig{
				ModelName: "gemini-3.1-flash-lite",
				RootAgent: "Agent Botson",
			}
			// Bootstrap the config file so it physically exists
			_ = Save(defaultCfg)
			return defaultCfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	if cfg.ModelName == "" {
		cfg.ModelName = "gemini-3.1-flash-lite"
	}

	return &cfg, nil
}

// Save writes the configuration to ~/.botsonv2/config.json.
func Save(cfg *AppConfig) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config JSON: %w", err)
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetDataDir resolves the physical path to ~/.botsonv2/ and ensures it exists.
func GetDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home directory: %w", err)
	}
	dataDir := filepath.Join(home, ".botsonv2")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}
	return dataDir, nil
}
