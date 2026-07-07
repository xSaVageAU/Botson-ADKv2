package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/config"
	coresession "botsonv2/core/session"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

func main() {
	// Redirect standard logger to a file to prevent polluting terminal interface
	logFile, errLog := os.OpenFile("tui.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if errLog == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	agentFlag := flag.String("agent", "", "Agent name to chat with (defaults to root agent)")
	flag.Parse()

	// Load config
	appConfig, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create Gemini model
	geminiModel, err := gemini.NewModel(ctx, appConfig.ModelName, &genai.ClientConfig{
		APIKey: appConfig.GeminiAPIKey,
	})
	if err != nil {
		fmt.Printf("Error creating Gemini model: %v\n", err)
		os.Exit(1)
	}

	// Load agents
	loader, err := agent.LoadDefaultAgents(geminiModel)
	if err != nil {
		fmt.Printf("Error loading agents: %v\n", err)
		os.Exit(1)
	}

	// Resolve agent to use
	var loadedAgent adkagent.Agent
	targetAgentName := *agentFlag
	if targetAgentName == "" {
		loadedAgent = loader.RootAgent()
		if loadedAgent == nil {
			fmt.Println("Error: No root agent loaded in this workspace.")
			os.Exit(1)
		}
		targetAgentName = loadedAgent.Name()
	} else {
		var err error
		loadedAgent, err = loader.LoadAgent(targetAgentName)
		if err != nil {
			fmt.Printf("Error finding agent %q: %v\n", targetAgentName, err)
			os.Exit(1)
		}
	}

	// Resolve data directory
	dataDir, err := config.GetDataDir()
	if err != nil {
		fmt.Printf("Error resolving data directory: %v\n", err)
		os.Exit(1)
	}

	// Session & Artifact services
	dbSessionService, err := coresession.InitPersistentSessionService(dataDir)
	if err != nil {
		fmt.Printf("Error loading session database: %v\n", err)
		os.Exit(1)
	}
	silenceGormLogger(dbSessionService)

	localArtifactService, err := coreartifact.NewLocalFileService(dataDir)
	if err != nil {
		fmt.Printf("Error loading artifact service: %v\n", err)
		os.Exit(1)
	}

	// Create active session
	sessionID := uuid.New().String()
	_, err = dbSessionService.Create(ctx, &session.CreateRequest{
		AppName:   targetAgentName,
		UserID:    "user",
		SessionID: sessionID,
		State: map[string]any{
			"__session_metadata__": map[string]any{
				"displayName": fmt.Sprintf("TUI Session - %s", targetAgentName),
			},
		},
	})
	if err != nil {
		fmt.Printf("Error creating chat session: %v\n", err)
		os.Exit(1)
	}

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:           targetAgentName,
		Agent:             loadedAgent,
		SessionService:    dbSessionService,
		ArtifactService:   localArtifactService,
		AutoCreateSession: true,
	})
	if err != nil {
		fmt.Printf("Error building runner: %v\n", err)
		os.Exit(1)
	}

	// Initialize Bubble Tea model
	m := initialModel(r, sessionID, targetAgentName)

	program = tea.NewProgram(m, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		log.Fatal(err)
	}
}
