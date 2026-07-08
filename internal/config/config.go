package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AppConfig holds the application configuration.
type AppConfig struct {
	ModelName    string        `json:"model_name"`
	GeminiAPIKey string        `json:"gemini_api_key"`
	RootAgent    string        `json:"root_agent"`
	Discord      DiscordConfig `json:"discord"`

	// DefaultCommand is which subcommand a bare `botson` (no args) runs --
	// "tui", "web", or "discord". Settable via `botson setup install`,
	// `botson settings set --default-command`, or the updateSettings agent
	// tool. Empty means "tui".
	DefaultCommand string `json:"default_command"`

	// WorkspaceDir is the directory background/detached processes (tray,
	// and anything tray itself spawns) operate in when they have no
	// meaningful working directory of their own to inherit -- e.g. tray
	// launched via a login-time autostart entry. Set once by `setup
	// install` (defaulting to wherever install was run from) or `botson
	// settings set`. Processes launched directly from a terminal (`botson
	// web start`, `botson discord start`) instead use their own actual
	// cwd and ignore this field entirely -- it exists only for the cases
	// that have no real cwd to fall back on.
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

// MaskedSecret is the placeholder Mask substitutes for secret fields, and
// what UpdateConfig-style callers should treat as "unchanged, keep the
// existing value" when they see it come back in a request.
const MaskedSecret = "******"

// Mask returns a copy of cfg with secret fields (Gemini API key, Discord
// token) replaced by MaskedSecret, so it's safe to hand to a UI or an
// agent tool. Lives here rather than in internal/management so internal/tools can
// use it too without an import cycle (tools -> management -> agent ->
// tools).
func Mask(cfg *AppConfig) AppConfig {
	masked := *cfg
	if masked.GeminiAPIKey != "" {
		masked.GeminiAPIKey = MaskedSecret
	}
	if masked.Discord.Token != "" {
		masked.Discord.Token = MaskedSecret
	}
	return masked
}

// DiscordConfig holds parameters for the Discord gateway integration.
// Whether the gateway is running is controlled entirely by the
// discord start/stop background daemon (or the webui's Start/Stop
// buttons, which call the same daemon) -- not by a config flag.
type DiscordConfig struct {
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

// mu guards cached, the shared in-process configuration instance. Every
// Load within a single process returns this same *AppConfig after the
// first call, and Update mutates its fields in place (rather than
// replacing the pointer) so every other holder of it -- e.g. cmd/botson's
// appBoot.Config -- sees the change immediately, without waiting for a
// restart. Cross-process staleness (another botson process editing the
// same file) is unaffected: each process still only picks up disk changes
// made by others at its own next startup.
var (
	mu     sync.Mutex
	cached *AppConfig
)

// Load returns this process's shared configuration, reading it from
// ~/.botsonv2/config.json on the first call and returning the same cached
// instance on every call after that. If the file does not exist yet, it's
// bootstrapped with a default template.
func Load() (*AppConfig, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked()
}

func loadLocked() (*AppConfig, error) {
	if cached != nil {
		return cached, nil
	}

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
			if err := saveLocked(defaultCfg); err != nil {
				return nil, err
			}
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

	cached = &cfg
	return cached, nil
}

// Save persists cfg to disk and becomes this process's shared cached
// instance from then on. Prefer Update when mutating fields already
// obtained from Load, so the change applies in place instead of
// orphaning whatever pointer other code is holding.
func Save(cfg *AppConfig) error {
	mu.Lock()
	defer mu.Unlock()
	return saveLocked(cfg)
}

func saveLocked(cfg *AppConfig) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config JSON: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	cached = cfg
	return nil
}

// Update applies mutate to the shared, already-loaded configuration and
// persists the result in one atomic step. Because mutate edits the
// existing cached instance's fields in place (rather than Update building
// a new struct and replacing the pointer), the change is immediately
// visible to every other holder of that pointer within this process --
// e.g. a tool letting the running agent change its own settings mid-chat.
func Update(mutate func(cfg *AppConfig)) (*AppConfig, error) {
	mu.Lock()
	defer mu.Unlock()

	cfg, err := loadLocked()
	if err != nil {
		return nil, err
	}
	mutate(cfg)
	if err := saveLocked(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
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
