package main

import (
	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/config"
	coresession "botsonv2/core/session"
	"botsonv2/core/webui"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/universal"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/cmd/launcher/web/a2a"
	"google.golang.org/adk/v2/cmd/launcher/web/api"
	"google.golang.org/adk/v2/model/gemini"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Define standard flags directly to drop positional ADK parameters
	portFlag := flag.Int("port", 8080, "Port to run the unified server on")
	otelFlag := flag.Bool("otel_to_cloud", false, "Enables OpenTelemetry export to Google Cloud")
	flag.Parse()

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

	// We configure the launcher with only ADK's production sublaunchers (REST and A2A) and our custom console
	customLauncher := universal.NewLauncher(
		web.NewLauncher(
			api.NewLauncher(),
			a2a.NewLauncher(),
			webui.NewSublauncher(),
		),
	)

	// Execute the Unified REST & UI Web server
	fmt.Printf("Starting production server on http://localhost:%d... please do not close this window.\n", *portFlag)
	
	args := []string{
		"web",
		fmt.Sprintf("-port=%d", *portFlag),
		fmt.Sprintf("-otel_to_cloud=%t", *otelFlag),
		"api", "a2a", "botson",
	}
	
	if err = customLauncher.Execute(ctx, configLauncher, args); err != nil {
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
