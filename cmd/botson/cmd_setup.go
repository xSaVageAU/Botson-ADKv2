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
	cmd.AddCommand(newSetupInstallCmd(), newSetupUninstallCmd(), newSetupResetCmd())
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
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Botson from PATH/startup and delete the installed binary (keeps your config/data)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Uninstall(cmd.Context())
		},
	}
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
