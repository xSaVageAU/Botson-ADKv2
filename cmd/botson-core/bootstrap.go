package main

import (
	"context"
	"fmt"
	"os"

	"botson/internal/agent"
	coreartifact "botson/internal/artifact"
	"botson/internal/config"
	"botson/internal/providers"
	coresession "botson/internal/session"
	"botson/internal/tools"

	"google.golang.org/adk/v2/cmd/launcher"
)

// appBoot bundles the shared services every subcommand needs: the loaded
// configuration and a launcher.Config wired up with the agent registry,
// session service, and artifact service.
type appBoot struct {
	Config   *config.AppConfig
	Launcher *launcher.Config
}

// setupApp loads configuration and constructs the shared agent/session/
// artifact wiring used by every subcommand (tui, web, discord), so the
// dispatcher only has to build it once regardless of which one runs.
func setupApp(ctx context.Context) (*appBoot, error) {
	appConfig, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}

	if err := os.MkdirAll(appConfig.WorkspaceRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace directory: %w", err)
	}
	tools.SetWorkspaceRoot(appConfig.WorkspaceRoot)

	model, err := providers.New(ctx, appConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	loader, err := agent.LoadDefaultAgents(model)
	if err != nil {
		return nil, fmt.Errorf("failed to load agents: %w", err)
	}

	dataDir, err := config.GetDataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve data directory: %w", err)
	}

	dbSessionService, err := coresession.InitPersistentSessionService(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session database: %w", err)
	}

	localArtifactService, err := coreartifact.NewLocalFileService(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize artifact service: %w", err)
	}

	return &appBoot{
		Config: appConfig,
		Launcher: &launcher.Config{
			SessionService:  dbSessionService,
			ArtifactService: localArtifactService,
			AgentLoader:     loader,
		},
	}, nil
}
