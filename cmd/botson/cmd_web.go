package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"botsonv2/core/daemon"
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

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start Botson's unified core: REST/A2A APIs, the web console, and (with `toggleDiscord`) the Discord gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeb(cmd.Context(), port, otelToCloud)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port to run the unified server on")
	cmd.Flags().BoolVar(&otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")

	cmd.AddCommand(newWebStartCmd(), newWebStopCmd(), newWebStatusCmd(), newWebDaemonChildCmd())
	return cmd
}

// webDaemonChildArgs builds the argv used to relaunch this executable as the
// detached __daemon-child process, carrying the same flags the user passed.
func webDaemonChildArgs(port int, otelToCloud bool) []string {
	return []string{
		"web", "__daemon-child",
		"--port=" + strconv.Itoa(port),
		"--otel_to_cloud=" + strconv.FormatBool(otelToCloud),
	}
}

// newWebDaemonChildCmd is the hidden entrypoint the detached background
// process actually runs; users invoke `start`/`stop`/`status` instead.
func newWebDaemonChildCmd() *cobra.Command {
	var port int
	var otelToCloud bool

	cmd := &cobra.Command{
		Use:    "__daemon-child",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonCtx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			ln, ctrlPort, err := daemon.StartControlListener(cancel)
			if err != nil {
				return fmt.Errorf("failed to start control listener: %w", err)
			}
			defer ln.Close()

			if err := daemon.WriteState(webDaemonName, daemon.State{
				PID:       os.Getpid(),
				Port:      ctrlPort,
				StartedAt: time.Now(),
				Meta:      map[string]string{"apiPort": strconv.Itoa(port)},
			}); err != nil {
				return fmt.Errorf("failed to write daemon state: %w", err)
			}
			defer daemon.RemoveState(webDaemonName)

			return runWeb(daemonCtx, port, otelToCloud)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port to run the unified server on")
	cmd.Flags().BoolVar(&otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")
	return cmd
}

func newWebStartCmd() *cobra.Command {
	var port int
	var otelToCloud bool

	cmd := &cobra.Command{
		Use:               "start",
		Short:             "Start the web console as a detached background process",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to resolve current directory: %w", err)
			}
			pid, logPath, err := daemon.Start(webDaemonName, webDisplayName, wd, webDaemonChildArgs(port, otelToCloud))
			if err != nil {
				return err
			}
			fmt.Printf("Started %s in background (pid %d).\nLogs: %s\n", webDisplayName, pid, logPath)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "Port to run the unified server on")
	cmd.Flags().BoolVar(&otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")
	return cmd
}

func newWebStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             "Stop the background web server",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Stop(webDaemonName, webDisplayName, force); err != nil {
				return err
			}
			fmt.Printf("%s offline.\n", webDisplayName)
			return nil
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
			status, err := daemon.GetStatus(webDaemonName, webDisplayName)
			if err != nil {
				return err
			}
			if !status.Running {
				fmt.Printf("%s: not running\n", webDisplayName)
				return nil
			}
			fmt.Printf("%s: running (pid %d, started %s)\n", webDisplayName, status.PID, status.StartedAt.Format(time.RFC3339))
			return nil
		},
	}
}

func runWeb(ctx context.Context, port int, otelToCloud bool) error {
	// We configure the launcher with only ADK's production sublaunchers (REST and A2A) and our custom console
	customLauncher := universal.NewLauncher(
		web.NewLauncher(
			api.NewLauncher(),
			a2a.NewLauncher(),
			webui.NewSublauncher(),
		),
	)

	fmt.Printf("Starting Botson web server on http://localhost:%d... please do not close this window.\n", port)

	args := []string{
		"web",
		fmt.Sprintf("-port=%d", port),
		fmt.Sprintf("-otel_to_cloud=%t", otelToCloud),
		"api", "a2a", "botson",
	}

	execErr := customLauncher.Execute(ctx, boot.Launcher, args)

	if execErr != nil && ctx.Err() == nil {
		return fmt.Errorf("web server execution failed: %w", execErr)
	}
	if ctx.Err() != nil {
		log.Println("Server stopped gracefully via signal.")
	}
	return nil
}
