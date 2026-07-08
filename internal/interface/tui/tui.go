package tui

import (
	"fmt"
	"strings"

	"botsonv2/internal/interface/apiclient"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newModel(client *apiclient.Client, sessionID, agentName string) model {
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
		client:     client,
		sessionID:  sessionID,
		agentName:  agentName,
		userStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		agentStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")),
		toolStyle:  lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("208")),
		hitlStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
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

		// Reserve exactly as many lines as the footer actually renders (plus one
		// blank separator line) instead of a hardcoded guess, so an unusually
		// long status line can never push the viewport past the terminal edge.
		footerHeight := lipgloss.Height(m.footerView())
		viewportHeight := msg.Height - footerHeight - 1
		if viewportHeight < 1 {
			viewportHeight = 1
		}
		m.viewport.Height = viewportHeight

		promptWidth := lipgloss.Width(m.textInput.Prompt)
		inputWidth := msg.Width - promptWidth - 1
		if inputWidth < 1 {
			inputWidth = 1
		}
		m.textInput.Width = inputWidth

		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()

	case tea.KeyMsg:
		if m.pendingHITL != nil {
			// Text input is intentionally not updated at all while a
			// confirmation is pending (see the `if !m.thinking &&
			// m.pendingHITL == nil` guard below) -- only y/n/enter/esc/
			// ctrl+c do anything until it's answered.
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "y", "enter":
				pending := *m.pendingHITL
				m.pendingHITL = nil
				m.thinking = true
				m.history = append(m.history, m.hitlStyle.Render(fmt.Sprintf("✓ Approved: %s", pending.toolName)))
				m.viewport.SetContent(m.renderHistory())
				m.viewport.GotoBottom()
				go m.resumeAfterConfirmation(pending.callID, true)
			case "n", "esc":
				pending := *m.pendingHITL
				m.pendingHITL = nil
				m.thinking = true
				m.history = append(m.history, m.hitlStyle.Render(fmt.Sprintf("✗ Denied: %s", pending.toolName)))
				m.viewport.SetContent(m.renderHistory())
				m.viewport.GotoBottom()
				go m.resumeAfterConfirmation(pending.callID, false)
			}
			break
		}

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

	case hitlPendingMsg:
		m.thinking = false
		m.pendingHITL = &hitlPendingMsg{callID: msg.callID, toolName: msg.toolName, hint: msg.hint}
		hint := msg.hint
		if hint == "" {
			hint = "The agent requires approval to execute this tool."
		}
		m.history = append(m.history, m.hitlStyle.Render(fmt.Sprintf("⚠ Permission requested: %s\n%s\n(y/enter to approve, n/esc to deny)", msg.toolName, hint)))
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()

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

	if !m.thinking && m.pendingHITL == nil {
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

// footerView renders the bottom status line (the text input, the
// "thinking" spinner, or a pending-confirmation prompt), constrained to
// the current terminal width so it can never overflow into an extra line
// the layout hasn't budgeted for.
func (m model) footerView() string {
	width := m.width
	if width <= 0 {
		width = 80
	}

	var content string
	switch {
	case m.pendingHITL != nil:
		content = m.hitlStyle.Render(fmt.Sprintf("Approve %s? [y/n]", m.pendingHITL.toolName))
	case m.thinking:
		content = m.spinner.View() + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("Agent is thinking...")
	default:
		content = m.textInput.View()
	}

	return lipgloss.NewStyle().MaxWidth(width).Render(content)
}

func (m model) View() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.viewport.View(), "", m.footerView())
}
