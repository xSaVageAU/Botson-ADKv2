package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"botsonv2/internal/daemon"
	"botsonv2/internal/interface/discord"
	webui "botsonv2/internal/interface/web"

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

	cmd.AddCommand(newWebStartCmd(), newWebStopCmd(), newWebStatusCmd())
	return cmd
}

// webDaemonChildArgs builds the argv used to relaunch this executable as a
// detached background process, carrying the same flags the user passed. It
// is exactly the plain `web` subcommand a user would type themselves --
// runWeb registers daemon state regardless of how it was launched (see its
// doc comment), so there's no separate hidden child command to maintain.
func webDaemonChildArgs(port int, otelToCloud bool) []string {
	return []string{
		"web",
		"--port=" + strconv.Itoa(port),
		"--otel_to_cloud=" + strconv.FormatBool(otelToCloud),
	}
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

// runWeb starts Botson's unified core and registers it in the shared
// daemon-state/control-channel system (internal/daemon) so `botson web
// status/stop` -- and other clients looking for a core to attach to, like
// `botson tui` -- can find and manage it. This happens no matter how the
// process was launched: directly (`botson web`), detached (`web start`),
// bare `botson` defaulting to web, or under an external supervisor like
// systemd (a plain `ExecStart=botson web` unit works fine here -- systemd
// doesn't need this process to self-detach).
//
// Use runCoreServer directly instead of this for a private, unregistered
// core -- that's what cmd_tui.go's startEmbeddedCore does, since a TUI's
// own auto-started fallback core must NOT be discoverable/stoppable this
// way (see ensureCoreRunning).
func runWeb(ctx context.Context, port int, otelToCloud bool) error {
	daemonCtx, cancel := context.WithCancel(ctx)
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

	return runCoreServer(daemonCtx, port, otelToCloud, false)
}

// runCoreServer is the actual core -- REST/A2A APIs, the web console, and
// Discord-toggle wiring -- with no daemon-state registration of its own.
// quiet suppresses the startup banner, for the embedded-in-TUI case where
// it would otherwise print stray output just before the TUI's alt-screen
// takes over the terminal.
func runCoreServer(ctx context.Context, port int, otelToCloud bool, quiet bool) error {
	// Register this process as Botson's core so the Discord gateway can be
	// started/stopped in-process (internal/interface/discord/singleton.go)
	// instead of as a separate OS process.
	discord.InitCore(boot.Launcher)

	// We configure the launcher with only ADK's production sublaunchers (REST and A2A) and our custom console
	customLauncher := universal.NewLauncher(
		web.NewLauncher(
			api.NewLauncher(),
			a2a.NewLauncher(),
			webui.NewSublauncher(),
		),
	)

	if !quiet {
		fmt.Printf("Starting Botson web server on http://localhost:%d... please do not close this window.\n", port)
	}

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
	if ctx.Err() != nil && !quiet {
		log.Println("Server stopped gracefully via signal.")
	}
	return nil
}
