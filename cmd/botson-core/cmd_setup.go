package main

import (
	"botsonv2/internal/setup"

	"github.com/spf13/cobra"
)

// newSetupCmd is the one local, direct-to-disk bootstrap step: writing
// ~/.botsonv2/config.json (Gemini API key, above all) before any core or
// NATS server exists for a client to configure that over. Everything else
// about running Botson happens over NATS once a core is up -- see
// internal/natsapi.
func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "setup",
		Short:             "Write Botson's initial configuration",
		PersistentPreRunE: noBootstrap,
	}
	cmd.AddCommand(newSetupInstallCmd())
	return cmd
}

func newSetupInstallCmd() *cobra.Command {
	var (
		nonInteractive bool
		geminiAPIKey   string
		modelName      string
		rootAgent      string
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Interactively configure Botson (Gemini API key, root agent)",
		Long: "Interactively configure Botson: Gemini API key, then root agent.\n\n" +
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
			}
			return setup.Install(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip all prompts; use the flags below instead of asking")
	cmd.Flags().StringVar(&geminiAPIKey, "gemini-api-key", "", "Gemini API key (required on a first install if --non-interactive)")
	cmd.Flags().StringVar(&modelName, "model", "", "Gemini model name (default: gemini-3.1-flash-lite)")
	cmd.Flags().StringVar(&rootAgent, "root-agent", "", "Root agent name (default: Agent Botson)")

	return cmd
}
