package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/adk/v2/runner"
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
	userStyle  lipgloss.Style
	agentStyle lipgloss.Style
	toolStyle  lipgloss.Style
}
