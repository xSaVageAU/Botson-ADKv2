package main

import (
	"context"
	"fmt"
	"log"

	"botsonv2/core/interface/discord"
	webui "botsonv2/core/interface/web"

	"github.com/spf13/cobra"
	"google.golang.org/adk/v2/cmd/launcher/universal"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/cmd/launcher/web/a2a"
	"google.golang.org/adk/v2/cmd/launcher/web/api"
)

func newWebCmd() *cobra.Command {
	var port int
	var otelToCloud bool
	var withDiscord bool

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the unified web console with REST & A2A APIs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeb(cmd.Context(), port, otelToCloud, withDiscord)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port to run the unified server on")
	cmd.Flags().BoolVar(&otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")
	cmd.Flags().BoolVar(&withDiscord, "discord", false, "Start the background Discord Gateway alongside the web server")
	return cmd
}

func runWeb(ctx context.Context, port int, otelToCloud, withDiscord bool) error {
	// We configure the launcher with only ADK's production sublaunchers (REST and A2A) and our custom console
	customLauncher := universal.NewLauncher(
		web.NewLauncher(
			api.NewLauncher(),
			a2a.NewLauncher(),
			webui.NewSublauncher(),
		),
	)

	// Initialize the Discord Gateway manager so the console's Discord routes work
	mgr := discord.InitManager(boot.Launcher)

	discordEnabled := boot.Config.Discord.Enabled || withDiscord
	if discordEnabled {
		token := boot.Config.Discord.Token
		if token == "" {
			log.Println("Discord Warning: Discord integration is enabled, but Token is empty in config.json. Gateway disabled.")
		} else {
			log.Println("Starting background Discord Gateway via manager in background...")
			go func() {
				if err := mgr.Start(token); err != nil {
					log.Printf("Discord Error: failed to start gateway: %v\n", err)
				} else {
					log.Println("Discord Gateway is online in the background.")
				}
			}()
		}
	}

	fmt.Printf("Starting Botson web server on http://localhost:%d... please do not close this window.\n", port)

	args := []string{
		"web",
		fmt.Sprintf("-port=%d", port),
		fmt.Sprintf("-otel_to_cloud=%t", otelToCloud),
		"api", "a2a", "botson",
	}

	execErr := customLauncher.Execute(ctx, boot.Launcher, args)

	// Stop the Discord Gateway synchronously before the process terminates
	if mgr.IsRunning() {
		log.Println("Shutting down background Discord Gateway...")
		_ = mgr.Stop()
	}

	if execErr != nil && ctx.Err() == nil {
		return fmt.Errorf("web server execution failed: %w", execErr)
	}
	if ctx.Err() != nil {
		log.Println("Server stopped gracefully via signal.")
	}
	return nil
}
