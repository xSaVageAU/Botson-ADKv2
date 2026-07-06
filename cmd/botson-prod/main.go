package main

import (
	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/config"
	coresession "botsonv2/core/session"
	"botsonv2/core/webui/builder"
	"botsonv2/core/webui/chat"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/prod"
	"google.golang.org/adk/v2/model/gemini"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	prodLauncher := prod.NewLauncher()

	// 1. Start the Agent Builder background service (port :8081)
	go func() {
		builderPort := os.Getenv("BUILDER_PORT")
		if builderPort == "" {
			builderPort = ":8081"
		}
		log.Printf("Starting Agent Builder background service on http://localhost%s\n", builderPort)
		if err := builder.StartServerGracefully(ctx, builderPort); err != nil {
			log.Printf("Agent Builder background service error: %v\n", err)
		}
	}()

	// 2. Start the Custom Chat background service (port :8082)
	go func() {
		chatPort := os.Getenv("CHAT_PORT")
		if chatPort == "" {
			chatPort = ":8082"
		}
		log.Printf("Starting Custom Chat Interface background service on http://localhost%s\n", chatPort)
		if err := chat.StartServerGracefully(ctx, chatPort); err != nil {
			log.Printf("Custom Chat Interface background service error: %v\n", err)
		}
	}()

	// 3. Execute the Production ADK REST API (port :8080)
	fmt.Println("Starting production services... please do not close this window.")
	
	// Pass arguments to Execute: "api" launches REST API, and we set -webui_address to allow chat UI requests
	args := []string{"api", "-webui_address", "http://localhost:8082"}
	
	if err = prodLauncher.Execute(ctx, configLauncher, args); err != nil {
		if ctx.Err() != nil {
			log.Println("Server stopped gracefully via signal.")
		} else {
			log.Printf("Production server execution failed: %v\n", err)
			fmt.Println("Press Enter to exit...")
			fmt.Scanln()
			os.Exit(1)
		}
	}
}
