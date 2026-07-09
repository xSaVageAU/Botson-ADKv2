package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"botson/internal/daemon"
	"botson/internal/natsapi"
	"botson/internal/procutil"

	adkproxy "github.com/Savs-Agents/NATS-ADK-Proxy"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const coreDaemonName = "core"
const coreDisplayName = "Botson core"

// newCoreCmd starts Botson's core: the only process that ever holds the
// Gemini model, agent registry, and session/artifact services, and the
// only thing any consumer -- a Discord bot, a web UI, anything -- ever
// talks to. It's an embedded NATS server plus two subject namespaces on
// top of it: "adk.*" (an imported github.com/Savs-Agents/NATS-ADK-Proxy,
// fronting the real ADK REST/A2A surface) and "botson.*"
// (internal/natsapi, for settings/agents/sessions/dashboard -- state
// that isn't part of stock ADK's own API). There is no other
// interface in this binary; nothing about this command dispatches to a
// TUI or any other in-process consumer.
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

// runCore starts Botson's core and registers it in the shared
// daemon-state/control-channel system (internal/daemon) so `botson core
// status/stop` can find and manage it. This happens no matter how the
// process was launched: directly (`botson core`), detached (`core start`),
// or under an external supervisor like systemd (a plain `ExecStart=botson
// core` unit works fine here -- systemd doesn't need this process to
// self-detach).
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

// runCoreServer is the actual core -- an embedded NATS server plus the two
// subject namespaces described on newCoreCmd -- with no daemon-state
// registration of its own. quiet suppresses the startup banner. Blocks
// until ctx is done (or either namespace's server exits unexpectedly),
// then shuts everything down.
func runCoreServer(ctx context.Context, port int, quiet bool) error {
	srv, err := server.NewServer(&server.Options{
		Host:          "127.0.0.1",
		Port:          port,
		NoLog:         quiet,
		Authorization: boot.Config.NatsAuthToken,
	})
	if err != nil {
		return fmt.Errorf("failed to configure the embedded NATS server: %w", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		return fmt.Errorf("embedded NATS server never became ready")
	}
	defer srv.Shutdown()

	// The server's own Authorization token (above) gates every connection,
	// including this in-process one -- without passing it here, the core's
	// own adkproxy/natsapi wiring would be rejected by its own server.
	nc, err := nats.Connect(srv.ClientURL(), nats.Token(boot.Config.NatsAuthToken))
	if err != nil {
		return fmt.Errorf("failed to connect to the embedded NATS server: %w", err)
	}
	defer nc.Close()

	if !quiet {
		fmt.Printf("Starting Botson's core on nats://127.0.0.1:%d (token-authenticated)... please do not close this window.\n", port)
	}

	proxy, err := adkproxy.New(adkproxy.Config{
		NATSConn:      nc,
		ADK:           *boot.Launcher,
		SubjectPrefix: "adk",
		// The gateway's own per-request HTTP deadline to the local ADK
		// backend defaults to a fixed 30s (NATS-ADK-Proxy's
		// gateway.Options.RequestTimeout), which is shorter than
		// runCommand's own default subprocess timeout
		// (procutil.DefaultTimeout) -- a turn with even one
		// default-timeout runCommand call, or a few sequential
		// tool/model round trips, would blow past 30s and fail with
		// "context deadline exceeded" before the run itself actually
		// failed. Give it real headroom above the longest normal tool
		// call instead. Botson-TUI's own NATS request timeout
		// (adkclient.WithTimeout in its internal/natsapi/client.go)
		// must stay above this value too, or it becomes the new
		// bottleneck.
		RequestTimeout: procutil.DefaultTimeout + 90*time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to configure the ADK NATS gateway: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return proxy.Run(gctx) })
	g.Go(func() error { return natsapi.Serve(gctx, nc, boot.Launcher) })
	if err := g.Wait(); err != nil {
		return fmt.Errorf("core server execution failed: %w", err)
	}
	return nil
}
