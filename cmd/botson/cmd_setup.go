package main

import (
	"botsonv2/core/setup"

	"github.com/spf13/cobra"
)

// newSetupCmd groups the install/uninstall/reset lifecycle. None of these
// can assume a working config/Gemini/agent setup exists yet (installing is
// the whole point), so noBootstrap is set once here and inherited by all
// three children -- same pattern as the tray parent command.
func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "setup",
		Short:             "Install, uninstall, or reset this Botson installation",
		PersistentPreRunE: noBootstrap,
	}
	cmd.AddCommand(newSetupInstallCmd(), newSetupUninstallCmd(), newSetupResetCmd(), newSetupStatusCmd())
	return cmd
}

func newSetupInstallCmd() *cobra.Command {
	var (
		nonInteractive bool
		geminiAPIKey   string
		modelName      string
		rootAgent      string
		discord        bool
		discordToken   string
		discordOwnerID string
		trayAutostart  bool
		startTray      bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Interactively configure Botson and install it onto this machine",
		Long: "Interactively configure Botson and install it onto this machine.\n\n" +
			"Pass --non-interactive along with the flags below to drive this from a script " +
			"or another agent instead of answering prompts. Any flag left unset falls back " +
			"to whatever's already in the config (or a built-in default on a fresh install) " +
			"rather than prompting for it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := setup.InstallOptions{
				NonInteractive: nonInteractive,
				GeminiAPIKey:   geminiAPIKey,
				ModelName:      modelName,
				RootAgent:      rootAgent,
				DiscordToken:   discordToken,
				DiscordOwnerID: discordOwnerID,
			}
			if cmd.Flags().Changed("discord") {
				opts.Discord = &discord
			}
			if cmd.Flags().Changed("tray-autostart") {
				opts.RegisterTrayAutostart = &trayAutostart
			}
			if cmd.Flags().Changed("start-tray") {
				opts.StartTrayNow = &startTray
			}
			return setup.Install(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip all prompts; use the flags below instead of asking")
	cmd.Flags().StringVar(&geminiAPIKey, "gemini-api-key", "", "Gemini API key (required on a first install if --non-interactive)")
	cmd.Flags().StringVar(&modelName, "model", "", "Gemini model name (default: gemini-3.1-flash-lite)")
	cmd.Flags().StringVar(&rootAgent, "root-agent", "", "Root agent name (default: Agent Botson)")
	cmd.Flags().BoolVar(&discord, "discord", false, "Enable (true) or disable (false) Discord integration; omit to leave existing Discord config untouched")
	cmd.Flags().StringVar(&discordToken, "discord-token", "", "Discord bot token (required if --discord=true and none is already configured)")
	cmd.Flags().StringVar(&discordOwnerID, "discord-owner-id", "", "Discord owner user ID")
	cmd.Flags().BoolVar(&trayAutostart, "tray-autostart", false, "Register the tray icon to start at login (Windows only)")
	cmd.Flags().BoolVar(&startTray, "start-tray", false, "Start the tray icon immediately after install (Windows only)")

	return cmd
}

func newSetupUninstallCmd() *cobra.Command {
	var forceFull bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Selectively remove Botson from PATH/startup and/or delete the installed binary and data",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Uninstall(cmd.Context(), forceFull)
		},
	}
	cmd.Flags().BoolVar(&forceFull, "force-full-uninstall", false, "Skip all prompts and completely wipe ~/.botsonv2 (PATH, startup, binary, and config.json)")
	return cmd
}

func newSetupResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Selectively reset configuration and/or session data, then reconfigure",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Reset(cmd.Context())
		},
	}
}

func newSetupStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether Botson is installed, on PATH, and which services are running",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Status(cmd.Context())
		},
	}
}
