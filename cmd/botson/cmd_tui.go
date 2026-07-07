package main

import (
	"context"
	"fmt"

	tuiinterface "botsonv2/core/interface/tui"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start an interactive terminal chat session (default when no command is given)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context(), agentFlag)
		},
	}
}

func runTUI(ctx context.Context, agentName string) error {
	loader := boot.Launcher.AgentLoader

	var loadedAgent adkagent.Agent
	targetAgentName := agentName
	if targetAgentName == "" {
		loadedAgent = loader.RootAgent()
		if loadedAgent == nil {
			return fmt.Errorf("no root agent loaded in this workspace")
		}
		targetAgentName = loadedAgent.Name()
	} else {
		var err error
		loadedAgent, err = loader.LoadAgent(targetAgentName)
		if err != nil {
			return fmt.Errorf("finding agent %q: %w", targetAgentName, err)
		}
	}

	sessionID := uuid.New().String()
	_, err := boot.Launcher.SessionService.Create(ctx, &session.CreateRequest{
		AppName:   targetAgentName,
		UserID:    "tui",
		SessionID: sessionID,
		State: map[string]any{
			"__session_metadata__": map[string]any{
				"displayName": fmt.Sprintf("TUI Session - %s", targetAgentName),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("creating chat session: %w", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           targetAgentName,
		Agent:             loadedAgent,
		SessionService:    boot.Launcher.SessionService,
		ArtifactService:   boot.Launcher.ArtifactService,
		AutoCreateSession: true,
	})
	if err != nil {
		return fmt.Errorf("building runner: %w", err)
	}

	return tuiinterface.Run(r, boot.Launcher.SessionService, sessionID, targetAgentName)
}
