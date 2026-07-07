package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"botsonv2/core/daemon"
	"botsonv2/core/interface/discord"
	"botsonv2/core/management"

	"github.com/spf13/cobra"
)

const discordDaemonName = "discord"

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

			ln, port, err := daemon.StartControlListener(cancel)
			if err != nil {
				return fmt.Errorf("failed to start control listener: %w", err)
			}
			defer ln.Close()

			if err := daemon.WriteState(discordDaemonName, daemon.State{
				PID:       os.Getpid(),
				Port:      port,
				StartedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("failed to write daemon state: %w", err)
			}
			defer daemon.RemoveState(discordDaemonName)

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
			pid, logPath, err := management.StartDiscordDaemon()
			if err != nil {
				return err
			}
			fmt.Printf("Started Discord gateway in background (pid %d).\nLogs: %s\n", pid, logPath)
			return nil
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
			if err := management.StopDiscordDaemon(force); err != nil {
				return err
			}
			fmt.Println("Discord gateway offline.")
			return nil
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
			status, err := management.DiscordDaemonStatus()
			if err != nil {
				return err
			}
			if !status.Running {
				fmt.Println("Discord gateway: not running")
				return nil
			}
			fmt.Printf("Discord gateway: running (pid %d, started %s)\n", status.PID, status.StartedAt.Format(time.RFC3339))
			return nil
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
