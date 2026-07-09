// Package setup implements `botson setup install`: writing the initial
// ~/.botson/config.json a core needs before it can start (the Gemini API
// key, above all -- there's no NATS server yet at this point for a client
// to configure that over, so this one step has to stay a local,
// direct-to-disk operation). Plain functions, no Cobra awareness, so
// cmd/botson-core stays a thin wrapper -- same shape as internal/daemon and
// internal/management.
package setup

import (
	"context"
	"fmt"
	"strconv"

	"botson/internal/config"
	"botson/internal/management"
)

// InstallOptions carries flag-driven answers for a scripted
// (--non-interactive) install, so a human or another agent can drive
// `setup install` without a terminal attached for prompts. Any field left
// at its zero value is treated as "not answered" and falls back to
// whatever's already in the config (or the built-in default for a
// brand-new one) instead of prompting for it.
type InstallOptions struct {
	NonInteractive bool

	GeminiAPIKey string
	ModelName    string
	RootAgent    string
}

// Install writes ~/.botson/config.json: Gemini API key, then root agent.
// With opts.NonInteractive, prompts are skipped entirely in favor of opts.
func Install(ctx context.Context, opts InstallOptions) error {
	fmt.Println("Botson Setup - Install")
	fmt.Println("======================")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if opts.NonInteractive {
		if err := applyInstallOptions(cfg, opts); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save configuration: %w", err)
		}
		fmt.Println("Configuration saved.")
	} else {
		reconfigure := true
		if cfg.GeminiAPIKey != "" {
			reconfigure, err = AskYesNo("Existing configuration found. Reconfigure from scratch?", false)
			if err != nil {
				return err
			}
		}

		if reconfigure {
			if err := runConfigWizard(cfg); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}
			fmt.Println("Configuration saved.")
		}
	}

	fmt.Println()
	fmt.Println("Setup complete! Run `botson core start` to bring the core online, then")
	fmt.Println("talk to it over NATS -- see internal/natsapi/subjects.go and")
	fmt.Println("NATS-ADK-Proxy's README for the full subject list.")
	fmt.Println()
	fmt.Println("Workspace:      ", cfg.WorkspaceRoot)
	fmt.Println("NATS auth token:", cfg.NatsAuthToken)
	fmt.Println("A consumer on this machine (e.g. Botson-TUI) can read the token")
	fmt.Println("straight from ~/.botson/config.json to pair automatically; a remote")
	fmt.Println("consumer needs it copied over out of band.")
	return nil
}

// applyInstallOptions fills cfg from a non-interactive install's opts,
// leaving any field the caller didn't answer at its current (or
// freshly-loaded default) value rather than prompting for it.
func applyInstallOptions(cfg *config.AppConfig, opts InstallOptions) error {
	if opts.GeminiAPIKey != "" {
		cfg.GeminiAPIKey = opts.GeminiAPIKey
	}
	if cfg.GeminiAPIKey == "" {
		return fmt.Errorf("gemini API key required: pass --gemini-api-key (no existing configuration to fall back on)")
	}

	if opts.ModelName != "" {
		cfg.ModelName = opts.ModelName
	}

	if opts.RootAgent != "" {
		cfg.RootAgent = opts.RootAgent
	} else if cfg.RootAgent == "" {
		cfg.RootAgent = "Agent Botson"
	}

	return nil
}

// promptGeminiAPIKey asks for the Gemini API key (required to run anything).
func promptGeminiAPIKey(cfg *config.AppConfig) error {
	key, err := ReadMasked("Gemini API key")
	if err != nil {
		return err
	}
	cfg.GeminiAPIKey = key
	return nil
}

// promptRootAgent shows the real, currently-available agents (via
// management.ListAgents, which needs no Gemini model/API key) so the
// choice is validated against reality instead of free text.
func promptRootAgent(cfg *config.AppConfig) error {
	def := cfg.RootAgent
	if def == "" {
		def = "Agent Botson"
	}

	agents, err := management.ListAgents()
	if err != nil || len(agents) == 0 {
		name, err := ReadLine("Root agent name", def)
		if err != nil {
			return err
		}
		cfg.RootAgent = name
		return nil
	}

	fmt.Println("Available agents:")
	for i, a := range agents {
		marker := " "
		if a.Name == def {
			marker = "*"
		}
		fmt.Printf("  %s %d) %s\n", marker, i+1, a.Name)
	}

	choice, err := ReadLine("Choose a root agent (number or name)", def)
	if err != nil {
		return err
	}

	if idx, convErr := strconv.Atoi(choice); convErr == nil && idx >= 1 && idx <= len(agents) {
		cfg.RootAgent = agents[idx-1].Name
	} else {
		cfg.RootAgent = choice
	}
	return nil
}

// runConfigWizard runs the full set of prompts in order.
func runConfigWizard(cfg *config.AppConfig) error {
	if err := promptGeminiAPIKey(cfg); err != nil {
		return err
	}
	if err := promptRootAgent(cfg); err != nil {
		return err
	}
	return nil
}
