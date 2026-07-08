package tui

import (
	"strings"

	"botsonv2/core/interface/apiclient"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Package-level program reference to send background messages
var program *tea.Program

// Msg Types for Bubble Tea
type responseChunkMsg string
type responseDoneMsg struct{}
type responseErrMsg struct{ err error }
type toolCallMsg string

// hitlPendingMsg signals a confirmation-gated tool call is waiting for the
// user to approve or deny it before the agent can continue.
type hitlPendingMsg struct {
	callID   string
	toolName string
	hint     string
}

// hitlResolvedMsg is sent once the user has answered a pending
// confirmation, so Update can clear it and resume the run.
type hitlResolvedMsg struct{ approved bool }

type model struct {
	viewport        viewport.Model
	textInput       textinput.Model
	spinner         spinner.Model
	client          *apiclient.Client
	sessionID       string
	history         []string
	thinking        bool
	err             error
	width           int
	height          int
	agentName       string
	streamingOutput strings.Builder

	// pendingHITL is non-nil while a confirmation-gated tool call is
	// awaiting a yes/no answer; text input is disabled meanwhile (see
	// Update's tea.KeyMsg handling).
	pendingHITL *hitlPendingMsg

	// Styles
	userStyle  lipgloss.Style
	agentStyle lipgloss.Style
	toolStyle  lipgloss.Style
	hitlStyle  lipgloss.Style
}
