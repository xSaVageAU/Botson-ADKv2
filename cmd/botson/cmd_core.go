package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"botsonv2/internal/daemon"
	"botsonv2/internal/interface/natscore"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
)

const coreDaemonName = "core"
const coreDisplayName = "Botson core"

// newCoreCmd starts Botson's shared core: an embedded NATS server plus
// internal/interface/natscore's subject handlers, wrapping the same
// agent/session/artifact wiring every subcommand shares (see
// cmd/botson/bootstrap.go). Any interface -- this repo's TUI, and future
// standalone Discord/web projects -- talks to a running core purely over
// NATS; nothing about this command is HTTP-specific anymore.
func newCoreCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "core",
		Short: "Start Botson's shared core: the NATS API any interface talks to",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCore(cmd.Context(), port)
		},
	}
	cmd.Flags().IntVar(&port, "port", 4222, "Port to run the embedded NATS server on")

	cmd.AddCommand(newCoreStartCmd(), newCoreStopCmd(), newCoreStatusCmd())
	return cmd
}

// coreDaemonChildArgs builds the argv used to relaunch this executable as a
// detached background process, carrying the same flags the user passed. It
// is exactly the plain `core` subcommand a user would type themselves --
// runCore registers daemon state regardless of how it was launched (see its
// doc comment), so there's no separate hidden child command to maintain.
func coreDaemonChildArgs(port int) []string {
	return []string{"core", "--port=" + strconv.Itoa(port)}
}

func newCoreStartCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:               "start",
		Short:             "Start the core as a detached background process",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to resolve current directory: %w", err)
			}
			pid, logPath, err := daemon.Start(coreDaemonName, coreDisplayName, wd, coreDaemonChildArgs(port))
			if err != nil {
				return err
			}
			fmt.Printf("Started %s in background (pid %d).\nLogs: %s\n", coreDisplayName, pid, logPath)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 4222, "Port to run the embedded NATS server on")
	return cmd
}

func newCoreStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             "Stop the background core",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Stop(coreDaemonName, coreDisplayName, force); err != nil {
				return err
			}
			fmt.Printf("%s offline.\n", coreDisplayName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force-kill the background process instead of asking it to shut down gracefully")
	return cmd
}

func newCoreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Show whether the background core is running",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := daemon.GetStatus(coreDaemonName, coreDisplayName)
			if err != nil {
				return err
			}
			if !status.Running {
				fmt.Printf("%s: not running\n", coreDisplayName)
				return nil
			}
			fmt.Printf("%s: running (pid %d, started %s)\n", coreDisplayName, status.PID, status.StartedAt.Format(time.RFC3339))
			return nil
		},
	}
}

// runCore starts Botson's shared core and registers it in the shared
// daemon-state/control-channel system (internal/daemon) so `botson core
// status/stop` -- and other clients looking for a core to attach to, like
// `botson tui` -- can find and manage it. This happens no matter how the
// process was launched: directly (`botson core`), detached (`core start`),
// or under an external supervisor like systemd (a plain `ExecStart=botson
// core` unit works fine here -- systemd doesn't need this process to
// self-detach).
//
// Use runCoreServer directly instead of this for a private, unregistered
// core -- that's what cmd_tui.go's startEmbeddedCore does, since a TUI's
// own auto-started fallback core must NOT be discoverable/stoppable this
// way (see ensureCoreRunning).
func runCore(ctx context.Context, port int) error {
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ln, ctrlPort, err := daemon.StartControlListener(cancel)
	if err != nil {
		return fmt.Errorf("failed to start control listener: %w", err)
	}
	defer ln.Close()

	if err := daemon.WriteState(coreDaemonName, daemon.State{
		PID:       os.Getpid(),
		Port:      ctrlPort,
		StartedAt: time.Now(),
		Meta:      map[string]string{"natsPort": strconv.Itoa(port)},
	}); err != nil {
		return fmt.Errorf("failed to write daemon state: %w", err)
	}
	defer daemon.RemoveState(coreDaemonName)

	return runCoreServer(daemonCtx, port, false)
}

// runCoreServer is the actual core -- an embedded NATS server plus
// internal/interface/natscore's subject handlers -- with no daemon-state
// registration of its own. quiet suppresses the startup banner, for the
// embedded-in-TUI case where it would otherwise print stray output just
// before the TUI's alt-screen takes over the terminal. Blocks until ctx is
// done, then shuts the embedded server down.
func runCoreServer(ctx context.Context, port int, quiet bool) error {
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, NoLog: quiet})
	if err != nil {
		return fmt.Errorf("failed to configure the embedded NATS server: %w", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		return fmt.Errorf("embedded NATS server never became ready")
	}
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		return fmt.Errorf("failed to connect to the embedded NATS server: %w", err)
	}
	defer nc.Close()

	if !quiet {
		fmt.Printf("Starting Botson's core on nats://127.0.0.1:%d... please do not close this window.\n", port)
	}

	if err := natscore.Serve(ctx, nc, boot.Launcher); err != nil {
		return fmt.Errorf("core server execution failed: %w", err)
	}
	return nil
}
