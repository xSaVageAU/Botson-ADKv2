package main

import (
	"botsonv2/core/agent"
	"botsonv2/core/config"
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"google.golang.org/genai"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/cmd/launcher/web/api"
	"google.golang.org/adk/v2/cmd/launcher/web/webui"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/session"
)

// App struct
type App struct {
	ctx          context.Context
	webServerURL string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Load Configuration
	appConfig, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Initialize Gemini model
	model, err := gemini.NewModel(ctx, appConfig.ModelName, &genai.ClientConfig{
		APIKey: appConfig.GeminiAPIKey,
	})
	if err != nil {
		log.Fatalf("Failed to create Gemini model: %v", err)
	}

	assistantAgent, err := agent.NewAssistantAgent(model)
	if err != nil {
		log.Fatalf("Failed to create assistant agent: %v", err)
	}

	configLauncher := &launcher.Config{
		SessionService:  session.InMemoryService(),
		ArtifactService: artifact.InMemoryService(),
		AgentLoader:     adkagent.NewSingleLoader(assistantAgent),
	}

	webLauncher := web.NewLauncher(
		webui.NewLauncher(),
		api.NewLauncher(),
	)

	_, err = webLauncher.Parse([]string{"webui", "api"})
	if err != nil {
		log.Fatalf("Failed to initialize web launchers: %v", err)
	}

	a.webServerURL = "http://localhost:8080" // Standard port
	go func() {
		if err = webLauncher.Run(ctx, configLauncher); err != nil {
			log.Printf("Web server execution failed: %v", err)
		}
	}()

	// Wait for server to start
	for i := 0; i < 10; i++ {
		_, err := http.Get(a.webServerURL)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Greet is a placeholder for future functionality
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, ADK agent is initialized.", name)
}
