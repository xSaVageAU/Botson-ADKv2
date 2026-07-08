// Package tui implements Botson's terminal chat interface as a standalone
// Bubble Tea program. It follows the same shape as core/interface/discord and
// core/interface/web: callers assemble the shared agent/session/artifact
// plumbing and hand it to Run, rather than the interface building its own
// wiring.
package tui

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/adk/v2/runner"
)

// Run launches the interactive TUI chat program and blocks until the user
// exits.
func Run(r *runner.Runner, sessionID, agentName string) error {
	// Redirect standard logger to a file to prevent polluting the terminal interface
	logFile, errLog := os.OpenFile("tui.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if errLog == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	m := newModel(r, sessionID, agentName)

	program = tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}
