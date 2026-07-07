package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"botsonv2/core/interface/discord"
	webui "botsonv2/core/interface/web"

	"github.com/spf13/cobra"
	"google.golang.org/adk/v2/cmd/launcher/universal"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/cmd/launcher/web/a2a"
	"google.golang.org/adk/v2/cmd/launcher/web/api"
)

const webDaemonName = "web"
const webDisplayName = "Web server"

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

	cmd.AddCommand(newWebStartCmd(), newWebStopCmd(), newWebStatusCmd(), newWebDaemonChildCmd())
	return cmd
}

// webDaemonChildArgs builds the argv used to relaunch this executable as the
// detached __daemon-child process, carrying the same flags the user passed.
func webDaemonChildArgs(port int, otelToCloud, withDiscord bool) []string {
	return []string{
		"web", "__daemon-child",
		"--port=" + strconv.Itoa(port),
		"--otel_to_cloud=" + strconv.FormatBool(otelToCloud),
		"--discord=" + strconv.FormatBool(withDiscord),
	}
}

// newWebDaemonChildCmd is the hidden entrypoint the detached background
// process actually runs; users invoke `start`/`stop`/`status` instead.
func newWebDaemonChildCmd() *cobra.Command {
	var port int
	var otelToCloud bool
	var withDiscord bool

	cmd := &cobra.Command{
		Use:    "__daemon-child",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonCtx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			ln, ctrlPort, err := startControlListener(cancel)
			if err != nil {
				return fmt.Errorf("failed to start control listener: %w", err)
			}
			defer ln.Close()

			if err := writeDaemonState(webDaemonName, daemonState{
				PID:       os.Getpid(),
				Port:      ctrlPort,
				StartedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("failed to write daemon state: %w", err)
			}
			defer removeDaemonState(webDaemonName)

			return runWeb(daemonCtx, port, otelToCloud, withDiscord)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port to run the unified server on")
	cmd.Flags().BoolVar(&otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")
	cmd.Flags().BoolVar(&withDiscord, "discord", false, "Start the background Discord Gateway alongside the web server")
	return cmd
}

func newWebStartCmd() *cobra.Command {
	var port int
	var otelToCloud bool
	var withDiscord bool

	cmd := &cobra.Command{
		Use:               "start",
		Short:             "Start the web console as a detached background process",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startDaemon(webDaemonName, webDisplayName, webDaemonChildArgs(port, otelToCloud, withDiscord))
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port to run the unified server on")
	cmd.Flags().BoolVar(&otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")
	cmd.Flags().BoolVar(&withDiscord, "discord", false, "Start the background Discord Gateway alongside the web server")
	return cmd
}

func newWebStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             "Stop the background web server",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDaemon(webDaemonName, webDisplayName, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force-kill the background process instead of asking it to shut down gracefully")
	return cmd
}

func newWebStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Show whether the background web server is running",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printDaemonStatus(webDaemonName, webDisplayName)
		},
	}
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
