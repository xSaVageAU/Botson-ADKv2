package session

import (
	"fmt"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
)

// InitPersistentSessionService initializes a persistent GORM-backed session service
// that uses SQLite (via a CGO-free driver) stored inside the provided directory.
func InitPersistentSessionService(dataDir string) (session.Service, error) {
	dbPath := filepath.Join(dataDir, "sessions.db")

	// 1. Initialize SQLite dialector
	dialector := sqlite.Open(dbPath)

	// 2. Instantiate session service database backend
	sessionService, err := database.NewSessionService(dialector)
	if err != nil {
		return nil, fmt.Errorf("failed to create database session service: %w", err)
	}

	// 3. Auto-migrate table schemas
	if err := database.AutoMigrate(sessionService); err != nil {
		return nil, fmt.Errorf("failed to run database auto-migrations: %w", err)
	}

	return sessionService, nil
}
