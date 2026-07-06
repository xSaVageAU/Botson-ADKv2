package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/genai"

	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/config"
	"botsonv2/core/gateways/discord"
	coresession "botsonv2/core/session"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model/gemini"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load Configuration
	appConfig, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	discordToken := appConfig.Discord.Token
	if discordToken == "" {
		log.Fatal("Configuration error: discord.token is not defined in ~/.botsonv2/config.json.")
	}



	// Initialize Gemini model
	model, err := gemini.NewModel(ctx, appConfig.ModelName, &genai.ClientConfig{
		APIKey: appConfig.GeminiAPIKey,
	})
	if err != nil {
		log.Fatalf("Failed to create Gemini model: %v", err)
	}

	loader, err := agent.LoadDefaultAgents(model)
	if err != nil {
		log.Fatalf("Failed to load agents: %v", err)
	}

	// Resolve persistent data directory (~/.botsonv2)
	dataDir, err := config.GetDataDir()
	if err != nil {
		log.Fatalf("Failed to resolve data directory: %v", err)
	}

	// Initialize GORM-backed SQLite session service
	dbSessionService, err := coresession.InitPersistentSessionService(dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize session database: %v", err)
	}

	// Initialize local filesystem-backed artifact service
	localArtifactService, err := coreartifact.NewLocalFileService(dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize local artifact service: %v", err)
	}

	configLauncher := &launcher.Config{
		SessionService:  dbSessionService,
		ArtifactService: localArtifactService,
		AgentLoader:     loader,
	}

	// Create and Start the Discord Gateway client
	gateway, err := discord.New(discordToken, configLauncher)
	if err != nil {
		log.Fatalf("Failed to initialize Discord gateway: %v", err)
	}

	log.Println("Starting Standalone Discord Gateway...")
	if err := gateway.Start(); err != nil {
		log.Fatalf("Failed to start Discord gateway: %v", err)
	}

	log.Println("Discord Gateway is online. Press Ctrl+C to terminate.")

	// Wait for terminal signal to shut down gracefully
	<-ctx.Done()

	log.Println("Shutting down Discord Gateway gracefully...")
	if err := gateway.Close(); err != nil {
		log.Printf("Error closing gateway connection: %v", err)
	}
	log.Println("Discord Gateway offline. Good bye!")
}
