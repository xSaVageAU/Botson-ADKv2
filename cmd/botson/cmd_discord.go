package main

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"botsonv2/core/daemon"
	"botsonv2/core/interface/apiclient"
	"botsonv2/core/interface/discord"

	"github.com/spf13/cobra"
)

func newDiscordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discord",
		Short: "Start the standalone Discord gateway (foreground, its own separate process)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscord(cmd.Context())
		},
	}

	cmd.AddCommand(newDiscordStartCmd(), newDiscordStopCmd(), newDiscordStatusCmd())
	return cmd
}

// noBootstrap skips the root command's expensive config/Gemini/agent/session
// bootstrap for subcommands that only manage a background process's
// lifecycle and never touch the agent runtime themselves.
func noBootstrap(cmd *cobra.Command, args []string) error { return nil }

// discordCoreClient builds an HTTP client for the currently-running core.
// `discord start/stop/status` always mean "toggle/query Discord within
// the running core" now (see core/interface/discord/singleton.go) -- not
// "spawn a separate process" as they used to -- so there's nothing to
// fall back to if no core is running; erroring clearly here is better
// than silently doing something different from what the web console's
// own Discord Start/Stop buttons do.
func discordCoreClient() (*apiclient.Client, error) {
	status, _ := daemon.GetStatus(webDaemonName, webDisplayName)
	if !status.Running {
		return nil, fmt.Errorf("Botson's core isn't running; start it first with `botson web start` (or `botson tui`, which starts it automatically)")
	}
	apiPort := 8080
	if p, ok := status.Meta["apiPort"]; ok {
		if port, err := strconv.Atoi(p); err == nil {
			apiPort = port
		}
	}
	return apiclient.New(fmt.Sprintf("http://127.0.0.1:%d", apiPort)), nil
}

func newDiscordStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "start",
		Short:             "Start the Discord gateway within the running core",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := discordCoreClient()
			if err != nil {
				return err
			}
			if err := client.DiscordStart(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("Discord gateway started.")
			return nil
		},
	}
}

func newDiscordStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "stop",
		Short:             "Stop the Discord gateway running within the core",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := discordCoreClient()
			if err != nil {
				return err
			}
			if err := client.DiscordStop(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("Discord gateway offline.")
			return nil
		},
	}
}

func newDiscordStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Show whether the Discord gateway is running within the core",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := discordCoreClient()
			if err != nil {
				return err
			}
			running, err := client.DiscordStatus(cmd.Context())
			if err != nil {
				return err
			}
			if !running {
				fmt.Println("Discord gateway: not running")
				return nil
			}
			fmt.Println("Discord gateway: running")
			return nil
		},
	}
}

// runDiscord runs the Discord gateway as its own standalone, foreground
// process, independent of any core -- for anyone who genuinely wants
// Discord isolated (e.g. on a different machine than the TUI/web). Bare
// `botson discord` (no subcommand) still does the full Gemini/agent/
// session bootstrap itself, unlike start/stop/status above.
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
