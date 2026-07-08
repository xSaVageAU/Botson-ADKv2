package session

import (
	"fmt"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// InitPersistentSessionService initializes a persistent GORM-backed session service
// that uses SQLite (via a CGO-free driver) stored inside the provided directory.
func InitPersistentSessionService(dataDir string) (session.Service, error) {
	dbPath := filepath.Join(dataDir, "sessions.db")

	// 1. Initialize SQLite dialector
	dialector := sqlite.Open(dbPath)

	// 2. Instantiate session service database backend. GORM's default
	// logger writes to stdout (not stderr) -- silenced here, at the one
	// place every caller (CLI, TUI, web, Discord) constructs this service,
	// rather than each consumer having to work around it afterward (as the
	// TUI previously did via an unsafe-reflection hack, since the noise
	// would otherwise corrupt its alt-screen rendering -- and would just as
	// surely corrupt `botson sessions ... --json`'s stdout for any other
	// caller).
	sessionService, err := database.NewSessionService(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create database session service: %w", err)
	}

	// 3. Auto-migrate table schemas
	if err := database.AutoMigrate(sessionService); err != nil {
		return nil, fmt.Errorf("failed to run database auto-migrations: %w", err)
	}

	return sessionService, nil
}
