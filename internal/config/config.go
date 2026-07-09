package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AppConfig holds the application configuration.
type AppConfig struct {
	ModelName    string `json:"model_name"`
	GeminiAPIKey string `json:"gemini_api_key"`
	RootAgent    string `json:"root_agent"`

	// Provider selects which internal/providers backend builds the model.LLM
	// at boot: "gemini" (default) or "openrouter". ModelName is interpreted
	// differently depending on this: a bare Gemini model name for "gemini",
	// or a full OpenRouter model slug (e.g. "anthropic/claude-3.5-sonnet")
	// for "openrouter".
	Provider string `json:"provider"`
	// OpenRouterAPIKey is required when Provider == "openrouter".
	OpenRouterAPIKey string `json:"openrouter_api_key"`

	// WorkspaceRoot is the default directory the file/command tools
	// (listFiles, readFile, writeFile, editFile, runCommand) operate in
	// when a session hasn't set its own "botson:cwd" state override --
	// see internal/tools/workspace.go. Defaults to ~/.botson/workspace.
	WorkspaceRoot string `json:"workspace_root"`

	// NatsAuthToken gates the embedded NATS server -- required on every
	// connection once set (see cmd/botson-core/cmd_core.go). Generated
	// once and never exposed via Mask()/botson.settings.get, since it's
	// the credential that gates the very API that subject lives on.
	// omitempty so Mask()'s blanked copy drops the key entirely from a
	// botson.settings.get reply rather than showing an empty placeholder
	// -- on disk it's never actually empty once generated, so this has no
	// effect on the real config.json.
	NatsAuthToken string `json:"nats_auth_token,omitempty"`
}

// MaskedSecret is the placeholder Mask substitutes for secret fields, and
// what UpdateConfig-style callers should treat as "unchanged, keep the
// existing value" when they see it come back in a request.
const MaskedSecret = "******"

// Mask returns a copy of cfg with secret fields (the Gemini API key)
// replaced by MaskedSecret, so it's safe to hand to a UI or an agent tool.
// Lives here rather than in internal/management so internal/tools can use
// it too without an import cycle (tools -> management -> agent -> tools).
func Mask(cfg *AppConfig) AppConfig {
	masked := *cfg
	if masked.GeminiAPIKey != "" {
		masked.GeminiAPIKey = MaskedSecret
	}
	if masked.OpenRouterAPIKey != "" {
		masked.OpenRouterAPIKey = MaskedSecret
	}
	masked.NatsAuthToken = ""
	return masked
}

// generateToken returns a random hex token used as NatsAuthToken.
func generateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate NATS auth token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GetConfigPath returns the absolute path to ~/.botson/config.json
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
// replacing the pointer) so every other holder of it -- e.g. cmd/botson-core's
// appBoot.Config -- sees the change immediately, without waiting for a
// restart. Cross-process staleness (another botson process editing the
// same file) is unaffected: each process still only picks up disk changes
// made by others at its own next startup.
var (
	mu     sync.Mutex
	cached *AppConfig
)

// Load returns this process's shared configuration, reading it from
// ~/.botson/config.json on the first call and returning the same cached
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
				Provider:  "gemini",
			}
			if _, err := fillWorkspaceAndToken(defaultCfg); err != nil {
				return nil, err
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
	if cfg.Provider == "" {
		cfg.Provider = "gemini"
	}

	// Unlike ModelName above, WorkspaceRoot/NatsAuthToken must be persisted
	// to disk immediately once backfilled, not just fixed up in memory --
	// NatsAuthToken in particular is read directly off disk by other
	// processes (e.g. Botson-TUI pairing with a local core), so a value
	// that only exists in this process until some unrelated Save/Update
	// call is not good enough.
	dirty, err := fillWorkspaceAndToken(&cfg)
	if err != nil {
		return nil, err
	}
	if dirty {
		if err := saveLocked(&cfg); err != nil {
			return nil, err
		}
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

// GetDataDir resolves the physical path to ~/.botson/ and ensures it exists.
func GetDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home directory: %w", err)
	}
	dataDir := filepath.Join(home, ".botson")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}
	return dataDir, nil
}

// fillWorkspaceAndToken backfills WorkspaceRoot and NatsAuthToken on cfg if
// either is empty, reporting whether it changed anything so the caller
// knows to persist the result.
func fillWorkspaceAndToken(cfg *AppConfig) (dirty bool, err error) {
	if cfg.WorkspaceRoot == "" {
		dataDir, err := GetDataDir()
		if err != nil {
			return false, err
		}
		cfg.WorkspaceRoot = filepath.Join(dataDir, "workspace")
		dirty = true
	}

	if cfg.NatsAuthToken == "" {
		token, err := generateToken()
		if err != nil {
			return false, err
		}
		cfg.NatsAuthToken = token
		dirty = true
	}

	return dirty, nil
}
