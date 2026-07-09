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

// agentFlag is shared between the root command (bare `botson`, which
// always runs the TUI) and the explicit `tui` subcommand.
var agentFlag string

// resumeSessionFlag reattaches the TUI to an existing session instead of
// starting a new one -- shared the same way agentFlag is, so both bare
// `botson --session ID` and `botson tui --session ID` work.
var resumeSessionFlag string

// resumeUserFlag overrides the user ID a --session lookup is made under.
// New TUI sessions always run as "tui" (see runTUI), but a session being
// resumed may have been created by another interface under its own user
// ID -- the web console's default is literally "web" -- so resuming one
// of those needs this to not be hardcoded the same way.
var resumeUserFlag string

// noBootstrap skips the root command's expensive config/Gemini/agent/session
// bootstrap for subcommands that only manage a background process's
// lifecycle and never touch the agent runtime themselves.
func noBootstrap(cmd *cobra.Command, args []string) error { return nil }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "botson",
		Short: "Botson: an AI agent console",
		Long: "Botson is a terminal chat client (this binary) backed by a shared\n" +
			"core that speaks NATS -- run with no arguments to start a chat session.",
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
			return runTUI(cmd.Context(), agentFlag, resumeSessionFlag, resumeUserFlag)
		},
	}
	rootCmd.PersistentFlags().StringVar(&agentFlag, "agent", "", "Agent name to chat with (defaults to the configured root agent)")
	rootCmd.PersistentFlags().StringVar(&resumeSessionFlag, "session", "", "Resume an existing chat session by ID instead of starting a new one (see `botson sessions list`)")
	rootCmd.PersistentFlags().StringVar(&resumeUserFlag, "user", "tui", "User ID a --session lookup is made under (only relevant with --session; e.g. \"web\" for a session started in the web console)")

	rootCmd.AddCommand(newTUICmd(), newCoreCmd(), newTrayCmd(), newSetupCmd(), newSettingsCmd(), newAgentsCmd(), newScriptCmd(), newSessionsCmd())

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
