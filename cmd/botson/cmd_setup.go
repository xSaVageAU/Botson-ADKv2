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
	return &cobra.Command{
		Use:   "install",
		Short: "Interactively configure Botson and install it onto this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Install(cmd.Context())
		},
	}
}

func newSetupUninstallCmd() *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Selectively remove Botson from PATH/startup and/or delete the installed binary and data",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Uninstall(cmd.Context(), full)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "Also delete config.json without asking (a complete wipe of ~/.botsonv2)")
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
