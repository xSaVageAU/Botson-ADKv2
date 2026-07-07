package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"botsonv2/core/interface/discord"

	"github.com/spf13/cobra"
)

const discordDaemonName = "discord"
const discordDisplayName = "Discord gateway"

func newDiscordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discord",
		Short: "Start the standalone Discord gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscord(cmd.Context())
		},
	}

	cmd.AddCommand(newDiscordStartCmd(), newDiscordStopCmd(), newDiscordStatusCmd(), newDiscordDaemonChildCmd())
	return cmd
}

// noBootstrap skips the root command's expensive config/Gemini/agent/session
// bootstrap for subcommands that only manage a background process's
// lifecycle and never touch the agent runtime themselves.
func noBootstrap(cmd *cobra.Command, args []string) error { return nil }

// newDiscordDaemonChildCmd is the hidden entrypoint the detached background
// process actually runs; users invoke `start`/`stop`/`status` instead.
func newDiscordDaemonChildCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__daemon-child",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonCtx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			ln, port, err := startControlListener(cancel)
			if err != nil {
				return fmt.Errorf("failed to start control listener: %w", err)
			}
			defer ln.Close()

			if err := writeDaemonState(discordDaemonName, daemonState{
				PID:       os.Getpid(),
				Port:      port,
				StartedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("failed to write daemon state: %w", err)
			}
			defer removeDaemonState(discordDaemonName)

			return runDiscord(daemonCtx)
		},
	}
}

func newDiscordStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "start",
		Short:             "Start the Discord gateway as a detached background process",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startDaemon(discordDaemonName, discordDisplayName, []string{"discord", "__daemon-child"})
		},
	}
	return cmd
}

func newDiscordStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             "Stop the background Discord gateway",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDaemon(discordDaemonName, discordDisplayName, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force-kill the background process instead of asking it to shut down gracefully")
	return cmd
}

func newDiscordStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Show whether the background Discord gateway is running",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printDaemonStatus(discordDaemonName, discordDisplayName)
		},
	}
}

func runDiscord(ctx context.Context) error {
	token := boot.Config.Discord.Token
	if token == "" {
		return fmt.Errorf("discord.token is not defined in ~/.botsonv2/config.json")
	}

	gateway, err := discord.New(token, boot.Launcher)
	if err != nil {
		return fmt.Errorf("failed to initialize Discord gateway: %w", err)
	}

	log.Println("Starting Discord Gateway...")
	if err := gateway.Start(); err != nil {
		return fmt.Errorf("failed to start Discord gateway: %w", err)
	}
	log.Println("Discord Gateway is online. Press Ctrl+C to terminate.")

	<-ctx.Done()

	log.Println("Shutting down Discord Gateway gracefully...")
	if err := gateway.Close(); err != nil {
		log.Printf("Error closing gateway connection: %v", err)
	}
	log.Println("Discord Gateway offline. Good bye!")
	return nil
}
