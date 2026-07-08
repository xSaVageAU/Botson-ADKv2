package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"botsonv2/core/daemon"
	"botsonv2/core/interface/apiclient"
	tuiinterface "botsonv2/core/interface/tui"

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
	cmd.Flags().BoolVar(&noAutoStartCore, "no-auto-start", false, "Fail instead of auto-starting Botson's core if it isn't already running")
	return cmd
}

func runTUI(ctx context.Context, agentName string) error {
	apiPort, err := ensureCoreRunning()
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

// ensureCoreRunning makes sure Botson's shared core (the `web` daemon,
// which already serves everything a thin client needs) is up, auto-
// starting it -- from the calling process's own cwd, so the workspace is
// pinned to wherever the user actually is, not some ambient default --
// if it isn't already running, then returns the port its REST API is
// actually listening on (which may not be the default if the core was
// started with a non-default --port).
func ensureCoreRunning() (int, error) {
	status, _ := daemon.GetStatus(webDaemonName, webDisplayName)
	if !status.Running {
		if noAutoStartCore {
			return 0, fmt.Errorf("Botson's core isn't running and --no-auto-start was set; run `botson web start` first")
		}

		wd, err := os.Getwd()
		if err != nil {
			return 0, fmt.Errorf("failed to resolve current directory: %w", err)
		}
		if _, _, err := daemon.Start(webDaemonName, webDisplayName, wd, webDaemonChildArgs(8080, false)); err != nil {
			return 0, err
		}
		status, _ = daemon.GetStatus(webDaemonName, webDisplayName)
	}

	if p, ok := status.Meta["apiPort"]; ok {
		if port, err := strconv.Atoi(p); err == nil {
			return port, nil
		}
	}
	return 8080, nil
}
