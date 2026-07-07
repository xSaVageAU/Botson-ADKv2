package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/adk/v2/runner"
)

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
	vp.SetContent("")

	return model{
		viewport:   vp,
		textInput:  ti,
		spinner:    s,
		runner:     r,
		sessionID:  sessionID,
		agentName:  agentName,
		userStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		agentStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")),
		toolStyle:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("208")),
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

		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 3

		m.textInput.Width = msg.Width - 10
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

func (m model) renderHistory() string {
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	wrapStyle := lipgloss.NewStyle().Width(width)

	var sb strings.Builder
	for i, item := range m.history {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(wrapStyle.Render(item))
	}
	if m.thinking {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		agentPrefix := m.agentStyle.Render(fmt.Sprintf("[%s] > ", m.agentName))
		sb.WriteString(wrapStyle.Render(agentPrefix + m.streamingOutput.String()))
	}
	return sb.String()
}

func (m model) View() string {
	var sb strings.Builder

	// Viewport Content
	sb.WriteString(m.viewport.View() + "\n\n")

	// Input Footer / Spinner
	if m.thinking {
		sb.WriteString(m.spinner.View() + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("Agent is thinking..."))
	} else {
		sb.WriteString(m.textInput.View())
	}

	return sb.String()
}
