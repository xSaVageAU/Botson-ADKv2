package discord

import (
	"log"
	"os"
	"sync"

	"google.golang.org/adk/v2/cmd/launcher"
)

type Manager struct {
	gateway *Gateway
	mu      sync.Mutex
	config  *launcher.Config
}

var (
	globalManager *Manager
	managerOnce   sync.Once
)

// InitManager initializes the global Discord gateway manager.
func InitManager(config *launcher.Config) *Manager {
	managerOnce.Do(func() {
		globalManager = &Manager{
			config: config,
		}
	})
	return globalManager
}

// GetManager returns the global Discord gateway manager.
func GetManager() *Manager {
	return globalManager
}

// Start starts the gateway with the given token if it is not already running.
func (m *Manager) Start(token string, guildID, logChannelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gateway != nil {
		return nil // Already running
	}

	// Set temporary environment overrides so gateway startup picks them up correctly
	if guildID != "" {
		os.Setenv("DISCORD_GUILD_ID", guildID)
	} else {
		os.Unsetenv("DISCORD_GUILD_ID")
	}
	if logChannelID != "" {
		os.Setenv("DISCORD_LOG_CHANNEL_ID", logChannelID)
	} else {
		os.Unsetenv("DISCORD_LOG_CHANNEL_ID")
	}

	gw, err := New(token, m.config)
	if err != nil {
		return err
	}

	if err := gw.Start(); err != nil {
		return err
	}

	m.gateway = gw
	log.Println("Discord gateway started successfully via manager.")
	return nil
}

// Stop stops the active gateway if it is running.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gateway == nil {
		return nil
	}

	err := m.gateway.Close()
	m.gateway = nil
	log.Println("Discord gateway stopped successfully via manager.")
	return err
}

// IsRunning returns whether the gateway is currently active.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gateway != nil
}

// Restart applies config updates dynamically by stopping and starting the bot.
func (m *Manager) Restart(enabled bool, token, guildID, logChannelID string) error {
	_ = m.Stop()

	if enabled && token != "" {
		return m.Start(token, guildID, logChannelID)
	}
	return nil
}
