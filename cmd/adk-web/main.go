package main

import (
	"botsonv2/core/agent"
	"botsonv2/core/config"
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/cmd/launcher/web/api"
	"google.golang.org/adk/v2/cmd/launcher/web/webui"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/session"
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

	configLauncher := &launcher.Config{
		SessionService:  session.InMemoryService(),
		ArtifactService: artifact.InMemoryService(),
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

	fmt.Println("Starting server... please do not close this window.")
	if err = webLauncher.Run(ctx, configLauncher); err != nil {
		log.Printf("Web server execution failed: %v", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}
}
