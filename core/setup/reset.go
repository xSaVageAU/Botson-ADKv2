package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"botsonv2/core/config"
)

// Reset walks through each config category asking whether to keep it or
// replace it right there (reusing install's prompt* functions), then
// separately offers to wipe session history and custom agent data. It
// always ends with a valid, saved config -- ready to run immediately,
// even if everything was replaced.
func Reset(ctx context.Context) error {
	fmt.Println("Botson Setup - Reset")
	fmt.Println("====================")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	keepKey, err := AskYesNo("Keep your current Gemini API key?", true)
	if err != nil {
		return err
	}
	if !keepKey {
		if err := promptGeminiAPIKey(cfg); err != nil {
			return err
		}
	}

	keepAgent, err := AskYesNo("Keep your current root agent selection?", true)
	if err != nil {
		return err
	}
	if !keepAgent {
		if err := promptRootAgent(cfg); err != nil {
			return err
		}
	}

	keepDiscord, err := AskYesNo("Keep your current Discord settings?", true)
	if err != nil {
		return err
	}
	if !keepDiscord {
		if err := promptDiscordSettings(cfg); err != nil {
			return err
		}
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}
	fmt.Println("Configuration saved.")

	// Defaults to "keep" (no) since wiping session history/custom agents
	// is materially more destructive than rotating a config value.
	wipeData, err := AskYesNo("Also wipe session history and custom agents? This cannot be undone.", false)
	if err != nil {
		return err
	}
	if wipeData {
		if err := wipeSessionAndAgentData(); err != nil {
			return err
		}
		fmt.Println("Session history and custom agents cleared.")
	}

	fmt.Println()
	fmt.Println("Reset complete. Run `botson` to get started right away.")
	return nil
}

func wipeSessionAndAgentData() error {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}

	if err := os.Remove(filepath.Join(dataDir, "sessions.db")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove session database: %w", err)
	}

	if err := os.RemoveAll(filepath.Join(dataDir, "agents")); err != nil {
		return fmt.Errorf("failed to remove custom agents: %w", err)
	}

	return nil
}
