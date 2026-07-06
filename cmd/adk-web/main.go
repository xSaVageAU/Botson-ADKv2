package main

import (
	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/builder"
	"botsonv2/core/config"
	coresession "botsonv2/core/session"
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/cmd/launcher/web/api"
	"google.golang.org/adk/v2/cmd/launcher/web/webui"
	"google.golang.org/adk/v2/model/gemini"
)

func main() {
	ctx := context.Background()

	// Load Configuration
	appConfig, err := config.Load()
	if err != nil {
		log.Printf("Configuration error: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	// Initialize Gemini model
	model, err := gemini.NewModel(ctx, appConfig.ModelName, &genai.ClientConfig{
		APIKey: appConfig.GeminiAPIKey,
	})
	if err != nil {
		log.Printf("Failed to create Gemini model: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	loader, err := agent.LoadDefaultAgents(model)
	if err != nil {
		log.Printf("Failed to load agents: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	// Resolve persistent data directory (~/.botsonv2)
	dataDir, err := config.GetDataDir()
	if err != nil {
		log.Printf("Failed to resolve data directory: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	// Initialize GORM-backed SQLite session service
	dbSessionService, err := coresession.InitPersistentSessionService(dataDir)
	if err != nil {
		log.Printf("Failed to initialize session database: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	// Initialize local filesystem-backed artifact service
	localArtifactService, err := coreartifact.NewLocalFileService(dataDir)
	if err != nil {
		log.Printf("Failed to initialize local artifact service: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	configLauncher := &launcher.Config{
		SessionService:  dbSessionService,
		ArtifactService: localArtifactService,
		AgentLoader:     loader,
	}

	webLauncher := web.NewLauncher(
		webui.NewLauncher(),
		api.NewLauncher(),
	)

	_, err = webLauncher.Parse([]string{"webui", "api"})
	if err != nil {
		log.Printf("Failed to initialize web launchers: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	// Start the Agent Builder background service
	go func() {
		builderPort := os.Getenv("BUILDER_PORT")
		if builderPort == "" {
			builderPort = ":8081"
		}
		log.Printf("Starting Agent Builder background service on http://localhost%s\n", builderPort)
		if err := builder.StartServer(builderPort); err != nil {
			log.Printf("Agent Builder background service failed to run: %v\n", err)
		}
	}()

	fmt.Println("Starting server... please do not close this window.")
	if err = webLauncher.Run(ctx, configLauncher); err != nil {
		log.Printf("Web server execution failed: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}
}
