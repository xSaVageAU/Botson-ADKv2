package main

import (
	"fmt"

	"botsonv2/internal/config"
	"botsonv2/internal/management"

	"github.com/spf13/cobra"
)

// newSettingsCmd groups reading and changing Botson's configuration.
// Neither subcommand needs the Gemini model/agent registry bootstrapped --
// same reasoning as `setup` (a broken or absent config is exactly the
// thing `settings set` needs to be usable to fix).
func newSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "settings",
		Short:             "View or change Botson's configuration",
		PersistentPreRunE: noBootstrap,
	}
	cmd.AddCommand(newSettingsGetCmd(), newSettingsSetCmd())
	return cmd
}

func newSettingsGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show the current configuration (secrets masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := management.GetMaskedConfig()
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, cfg)
			}
			printSettingsSummary(cfg)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON instead of a formatted summary")
	return cmd
}

func newSettingsSetCmd() *cobra.Command {
	var (
		modelName      string
		rootAgent      string
		defaultCommand string
		geminiAPIKey   string
		discordToken   string
		discordOwnerID string
		asJSON         bool
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Change one or more configuration values (only the flags you pass are changed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("default-command") {
				switch defaultCommand {
				case "tui", "web", "discord":
				default:
					return fmt.Errorf("--default-command must be one of: tui, web, discord")
				}
			}

			cfg, err := config.Update(func(cfg *config.AppConfig) {
				if cmd.Flags().Changed("model") {
					cfg.ModelName = modelName
				}
				if cmd.Flags().Changed("root-agent") {
					cfg.RootAgent = rootAgent
				}
				if cmd.Flags().Changed("default-command") {
					cfg.DefaultCommand = defaultCommand
				}
				if cmd.Flags().Changed("gemini-api-key") {
					cfg.GeminiAPIKey = geminiAPIKey
				}
				if cmd.Flags().Changed("discord-token") {
					cfg.Discord.Token = discordToken
				}
				if cmd.Flags().Changed("discord-owner-id") {
					cfg.Discord.OwnerID = discordOwnerID
				}
			})
			if err != nil {
				return err
			}

			masked := config.Mask(cfg)
			if asJSON {
				return encodeJSON(cmd, masked)
			}
			fmt.Println("Settings updated.")
			printSettingsSummary(&masked)
			return nil
		},
	}

	cmd.Flags().StringVar(&modelName, "model", "", "Gemini model name")
	cmd.Flags().StringVar(&rootAgent, "root-agent", "", "Root agent name")
	cmd.Flags().StringVar(&defaultCommand, "default-command", "", "What a bare `botson` runs: tui, web, or discord")
	cmd.Flags().StringVar(&geminiAPIKey, "gemini-api-key", "", "Gemini API key")
	cmd.Flags().StringVar(&discordToken, "discord-token", "", "Discord bot token")
	cmd.Flags().StringVar(&discordOwnerID, "discord-owner-id", "", "Discord owner user ID")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the updated (masked) config as JSON instead of a summary")
	return cmd
}

func printSettingsSummary(cfg *config.AppConfig) {
	fmt.Printf("Model:            %s\n", orNone(cfg.ModelName, "(not set)"))
	fmt.Printf("Root agent:       %s\n", orNone(cfg.RootAgent, "(not set)"))
	fmt.Printf("Default command:  %s\n", orNone(cfg.DefaultCommand, "tui (default)"))
	fmt.Printf("Gemini API key:   %s\n", orNone(cfg.GeminiAPIKey, "(not set)"))
	fmt.Printf("Discord token:    %s\n", orNone(cfg.Discord.Token, "(not set)"))
	fmt.Printf("Discord owner ID: %s\n", orNone(cfg.Discord.OwnerID, "(not set)"))
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}
