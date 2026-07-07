package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// boot holds the shared config/agent/session/artifact wiring, populated once
// by rootCmd's PersistentPreRunE before any subcommand's RunE executes.
var boot *appBoot

// agentFlag is shared between the root command (bare `botson`, which behaves
// like `botson tui`) and the explicit `tui` subcommand.
var agentFlag string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "botson",
		Short: "Botson: multi-purpose AI agent console",
		Long: "Botson combines a terminal chat client, a web console, and a Discord\n" +
			"gateway in one binary. Run with no arguments to start a chat session.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			b, err := setupApp(cmd.Context())
			if err != nil {
				return err
			}
			boot = b
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context(), agentFlag)
		},
	}
	rootCmd.PersistentFlags().StringVar(&agentFlag, "agent", "", "Agent name to chat with (defaults to the configured root agent)")

	rootCmd.AddCommand(newTUICmd(), newWebCmd(), newDiscordCmd(), newTrayCmd())

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
