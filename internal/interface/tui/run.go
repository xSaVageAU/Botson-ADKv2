// Package tui implements Botson's terminal chat interface as a standalone
// Bubble Tea program. Unlike internal/interface/discord and internal/interface/web
// (which are handed an in-process agent/session/artifact wiring), the TUI
// is a thin client of Botson's shared core over HTTP -- callers assemble
// an apiclient.Client pointed at a running core and hand it to Run.
package tui

import (
	"log"
	"os"

	"botsonv2/internal/interface/apiclient"

	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the interactive TUI chat program and blocks until the user
// exits.
func Run(client *apiclient.Client, sessionID, agentName string) error {
	// Redirect standard logger to a file to prevent polluting the terminal interface
	logFile, errLog := os.OpenFile("tui.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if errLog == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	m := newModel(client, sessionID, agentName)

	program = tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}
