package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"botsonv2/core/config"
	"botsonv2/core/management"
)

// Install walks a user through first-time setup: Gemini API key, root
// agent, optional Discord integration, then copies the binary to its
// stable install location, adds it to PATH, and (Windows only) offers to
// register the tray icon to start at login.
func Install(ctx context.Context) error {
	fmt.Println("Botson Setup - Install")
	fmt.Println("======================")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

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

	if err := installBinary(); err != nil {
		return err
	}

	installDir, err := InstallDir()
	if err != nil {
		return err
	}
	if err := AddToPath(installDir); err != nil {
		fmt.Printf("Warning: failed to update PATH automatically: %v\n", err)
		fmt.Printf("Add this directory to your PATH manually: %s\n", installDir)
	}

	if runtime.GOOS == "windows" {
		registerTray, err := AskYesNo("Start the Botson tray icon automatically at login?", true)
		if err != nil {
			return err
		}
		if registerTray {
			binPath, err := InstalledBinaryPath()
			if err != nil {
				return err
			}
			if err := RegisterTrayAutostart(binPath); err != nil {
				fmt.Printf("Warning: failed to register tray autostart: %v\n", err)
			} else {
				fmt.Println("Tray icon will start automatically at login.")
			}
		}
	}

	fmt.Println()
	fmt.Println("Setup complete! Open a new terminal and run `botson` to get started.")
	return nil
}

// installBinary copies the currently running executable to its stable
// install path, unless it's already running from there.
func installBinary() error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable: %w", err)
	}

	dst, err := InstalledBinaryPath()
	if err != nil {
		return err
	}

	srcAbs, _ := filepath.EvalSymlinks(src)
	dstAbs, _ := filepath.EvalSymlinks(dst)
	if srcAbs != "" && srcAbs == dstAbs {
		fmt.Println("Already running from the installed location; skipping binary copy.")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open current executable: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create installed binary: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	fmt.Printf("Installed to %s\n", dst)
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

// promptDiscordSettings asks for Discord integration, skippable entirely.
func promptDiscordSettings(cfg *config.AppConfig) error {
	want, err := AskYesNo("Configure Discord integration now?", cfg.Discord.Token != "")
	if err != nil {
		return err
	}
	if !want {
		cfg.Discord.Token = ""
		cfg.Discord.OwnerID = ""
		return nil
	}

	token, err := ReadMasked("Discord bot token")
	if err != nil {
		return err
	}
	cfg.Discord.Token = token

	ownerID, err := ReadLine("Discord owner user ID", cfg.Discord.OwnerID)
	if err != nil {
		return err
	}
	cfg.Discord.OwnerID = ownerID
	return nil
}

// runConfigWizard runs the full set of prompts in order; Reset reuses the
// individual prompt* functions directly for whichever categories aren't
// kept, rather than calling this.
func runConfigWizard(cfg *config.AppConfig) error {
	if err := promptGeminiAPIKey(cfg); err != nil {
		return err
	}
	if err := promptRootAgent(cfg); err != nil {
		return err
	}
	if err := promptDiscordSettings(cfg); err != nil {
		return err
	}
	return nil
}
