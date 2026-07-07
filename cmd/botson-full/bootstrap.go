package main

import (
	"context"
	"fmt"

	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/config"
	coresession "botsonv2/core/session"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
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

	model, err := gemini.NewModel(ctx, appConfig.ModelName, &genai.ClientConfig{
		APIKey: appConfig.GeminiAPIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
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
