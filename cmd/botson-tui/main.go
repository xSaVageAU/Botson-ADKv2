package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"reflect"
	"syscall"
	"unsafe"

	"botsonv2/core/agent"
	coreartifact "botsonv2/core/artifact"
	"botsonv2/core/config"
	coresession "botsonv2/core/session"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Package-level program reference to send background messages
var program *tea.Program

// Msg Types for Bubble Tea
type responseChunkMsg string
type responseDoneMsg struct{}
type responseErrMsg struct{ err error }
type toolCallMsg string

type model struct {
	viewport        viewport.Model
	textInput       textinput.Model
	spinner         spinner.Model
	runner          *runner.Runner
	sessionID       string
	history         []string
	thinking        bool
	err             error
	width           int
	height          int
	agentName       string
	streamingOutput strings.Builder

	// Styles
	titleStyle     lipgloss.Style
	userStyle      lipgloss.Style
	agentStyle     lipgloss.Style
	toolStyle      lipgloss.Style
	containerStyle lipgloss.Style
}

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

func initialModel(r *runner.Runner, sessionID, agentName string) model {
	ti := textinput.New()
	ti.Placeholder = "Type a message... (or type '/exit' to quit)"
	ti.Focus()
	ti.CharLimit = 1000
	ti.Width = 80

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	vp := viewport.New(80, 20)
	vp.SetContent("Welcome to Botson TUI! Chatting with Agent: " + agentName + "\n\n")

	return model{
		viewport:   vp,
		textInput:  ti,
		spinner:    s,
		runner:     r,
		sessionID:  sessionID,
		agentName:  agentName,
		titleStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(lipgloss.Color("86")).PaddingBottom(1),
		userStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		agentStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")),
		toolStyle:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("208")),
		containerStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Compute border margins for viewport resize
		headerHeight := 3
		inputHeight := 3
		borderMargins := 4
		m.viewport.Width = msg.Width - borderMargins - 4
		m.viewport.Height = msg.Height - headerHeight - inputHeight - borderMargins

		m.textInput.Width = msg.Width - borderMargins - 10
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			val := strings.TrimSpace(m.textInput.Value())
			if val == "" {
				break
			}
			if val == "/exit" || val == "/quit" {
				return m, tea.Quit
			}

			// Append user message
			m.history = append(m.history, m.userStyle.Render("[User] > ")+val)
			m.textInput.SetValue("")
			m.thinking = true
			m.streamingOutput.Reset()

			m.viewport.SetContent(m.renderHistory())
			m.viewport.GotoBottom()

			// Launch runner goroutine
			go m.runAgentStream(val)
		}

	case spinner.TickMsg:
		m.spinner, spCmd = m.spinner.Update(msg)
		cmds = append(cmds, spCmd)

	case responseChunkMsg:
		m.streamingOutput.WriteString(string(msg))
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()

	case toolCallMsg:
		m.history = append(m.history, m.toolStyle.Render(fmt.Sprintf("⚙️ executing tool %s...", string(msg))))
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()

	case responseDoneMsg:
		m.thinking = false
		m.history = append(m.history, m.agentStyle.Render(fmt.Sprintf("[%s] > ", m.agentName))+m.streamingOutput.String())
		m.streamingOutput.Reset()
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()

	case responseErrMsg:
		m.thinking = false
		m.history = append(m.history, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("❌ Error: %v", msg.err)))
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()
	}

	if !m.thinking {
		m.textInput, tiCmd = m.textInput.Update(msg)
		cmds = append(cmds, tiCmd)
	}

	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m model) runAgentStream(text string) {
	ctx := context.Background()
	userMsg := genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: text}},
	}
	runIter := m.runner.Run(ctx, "user", m.sessionID, &userMsg, adkagent.RunConfig{})

	for event, err := range runIter {
		if err != nil {
			program.Send(responseErrMsg{err: err})
			return
		}
		if event == nil {
			continue
		}
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					program.Send(responseChunkMsg(part.Text))
				}
				if part.FunctionCall != nil {
					program.Send(toolCallMsg(part.FunctionCall.Name))
				}
			}
		}
	}
	program.Send(responseDoneMsg{})
}

func (m model) renderHistory() string {
	var sb strings.Builder
	sb.WriteString(strings.Join(m.history, "\n\n"))
	if m.thinking {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(m.agentStyle.Render(fmt.Sprintf("[%s] > ", m.agentName)) + m.streamingOutput.String())
	}
	return sb.String()
}

func (m model) View() string {
	var sb strings.Builder

	// Header Title
	sb.WriteString(m.titleStyle.Render(fmt.Sprintf("BOTSON TERMINAL CONSOLE  |  Agent: %s", m.agentName)) + "\n\n")

	// Viewport Content
	sb.WriteString(m.viewport.View() + "\n\n")

	// Input Footer / Spinner
	if m.thinking {
		sb.WriteString(m.spinner.View() + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("Agent is thinking..."))
	} else {
		sb.WriteString(m.textInput.View())
	}

	// AltScreen Container
	return m.containerStyle.Width(m.width - 6).Height(m.height - 4).Render(sb.String())
}

func silenceGormLogger(service interface{}) {
	val := reflect.ValueOf(service)
	if val.Kind() != reflect.Ptr {
		return
	}
	val = val.Elem()
	if val.Type().Name() != "databaseService" {
		return
	}
	dbField := val.FieldByName("db")
	if !dbField.IsValid() {
		return
	}

	ptr := unsafe.Pointer(dbField.UnsafeAddr())
	gormDB := *(**gorm.DB)(ptr)
	if gormDB != nil {
		gormDB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	}
}
