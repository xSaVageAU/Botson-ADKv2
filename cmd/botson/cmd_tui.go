package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"time"

	"botsonv2/internal/daemon"
	"botsonv2/internal/interface/apiclient"
	tuiinterface "botsonv2/internal/interface/tui"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var noAutoStartCore bool

// newTUICmd is a thin client of Botson's shared core, unlike web/discord --
// it doesn't need the full Gemini/agent/session bootstrap in its own
// process, so it opts out of rootCmd's PersistentPreRunE the same way the
// setup/settings/agents/script/sessions lifecycle commands already do.
func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "tui",
		Short:             "Start an interactive terminal chat session (default when no command is given)",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context(), agentFlag)
		},
	}
	cmd.Flags().BoolVar(&noAutoStartCore, "no-auto-start", false, "Fail instead of running a private, in-process core if Botson's shared core isn't already running")
	return cmd
}

func runTUI(ctx context.Context, agentName string) error {
	apiPort, err := ensureCoreRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to reach Botson's core: %w", err)
	}

	client := apiclient.New(fmt.Sprintf("http://127.0.0.1:%d", apiPort))

	targetAgentName := agentName
	if targetAgentName == "" {
		targetAgentName, err = client.DefaultAgent(ctx)
		if err != nil {
			return fmt.Errorf("finding the default agent: %w", err)
		}
	}

	sessionID := uuid.New().String()
	if _, err := client.CreateSession(ctx, targetAgentName, "tui", sessionID, map[string]any{
		"__session_metadata__": map[string]any{
			"displayName": fmt.Sprintf("TUI Session - %s", targetAgentName),
		},
	}); err != nil {
		return fmt.Errorf("creating chat session: %w", err)
	}

	return tuiinterface.Run(client, sessionID, targetAgentName)
}

// ensureCoreRunning finds a Botson core for the TUI to talk to, preferring
// a real, discoverable one (`botson web`, `web start`, or one managed by
// an external supervisor like systemd -- anything that calls runWeb, which
// always registers itself) if one is already running. Only if none is
// found does it fall back to a private, in-process core scoped to this
// TUI's own lifetime (see startEmbeddedCore) -- deliberately NOT a
// detached background daemon, since a bare `botson`/`botson tui` silently
// leaving a persistent background process running is exactly the surprise
// this is meant to avoid. Anyone who wants Discord/web actually running in
// the background sets that up explicitly (`web start`, a systemd unit,
// etc.), never as a side effect of opening the TUI.
func ensureCoreRunning(ctx context.Context) (int, error) {
	status, _ := daemon.GetStatus(webDaemonName, webDisplayName)
	if status.Running {
		if p, ok := status.Meta["apiPort"]; ok {
			if port, err := strconv.Atoi(p); err == nil {
				return port, nil
			}
		}
		return 8080, nil
	}

	if noAutoStartCore {
		return 0, fmt.Errorf("Botson's core isn't running and --no-auto-start was set; run `botson web start` first")
	}

	return startEmbeddedCore(ctx)
}

// startEmbeddedCore runs a full core (REST/A2A APIs, Discord toggle
// wiring) inside this same TUI process on an ephemeral loopback port, for
// this process's exclusive use. Unlike runWeb, it never calls
// daemon.WriteState -- nothing else can discover or stop it, and it
// disappears the instant this process exits, leaving nothing running in
// the background. It performs its own bootstrap (setupApp) if the TUI
// subcommand's own PersistentPreRunE skipped it (see noBootstrap) --
// becoming self-sufficient is exactly the right fallback when there's no
// shared core to be a thin client of.
func startEmbeddedCore(ctx context.Context) (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to allocate a local port for an embedded core: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		return 0, fmt.Errorf("failed to release the allocated port: %w", err)
	}

	if boot == nil {
		b, err := setupApp(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to initialize Botson: %w", err)
		}
		boot = b
	}

	// runCoreServer (and libraries it drives) log via the stdlib `log`
	// package; left alone, that output would land mid-frame in the TUI's
	// alt-screen and corrupt the display.
	log.SetOutput(io.Discard)

	failed := make(chan error, 1)
	go func() {
		failed <- runCoreServer(ctx, port, false, true)
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-failed:
			if err != nil {
				return 0, fmt.Errorf("embedded core failed to start: %w", err)
			}
			return 0, fmt.Errorf("embedded core exited immediately")
		default:
		}
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			conn.Close()
			return port, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("timed out waiting for the embedded core to start")
}
