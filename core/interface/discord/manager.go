package discord

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"botsonv2/core/config"
	"google.golang.org/adk/v2/cmd/launcher"
)

type PendingRequest struct {
	Code      string `json:"code"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	ChannelID string `json:"channel_id"`
}

type Manager struct {
	gateway      *Gateway
	mu           sync.Mutex
	config       *launcher.Config
	pendingAuths map[string]PendingRequest // code -> request
}

var (
	globalManager *Manager
	managerOnce   sync.Once
)

// InitManager initializes the global Discord gateway manager.
func InitManager(config *launcher.Config) *Manager {
	managerOnce.Do(func() {
		globalManager = &Manager{
			config:       config,
			pendingAuths: make(map[string]PendingRequest),
		}
	})
	return globalManager
}

// GetManager returns the global Discord gateway manager.
func GetManager() *Manager {
	return globalManager
}

// Start starts the gateway with the given token if it is not already running.
func (m *Manager) Start(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gateway != nil {
		return nil // Already running
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
func (m *Manager) Restart(enabled bool, token string) error {
	_ = m.Stop()

	if enabled && token != "" {
		return m.Start(token)
	}
	return nil
}

// GetPendingRequests returns the list of pending authorizations.
func (m *Manager) GetPendingRequests() []PendingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	var reqs []PendingRequest
	for _, r := range m.pendingAuths {
		reqs = append(reqs, r)
	}
	return reqs
}

// AddPendingRequest registers a pending user authentication request and returns the AUTH code.
func (m *Manager) AddPendingRequest(userID, username, channelID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already exists, return the existing code
	for _, req := range m.pendingAuths {
		if req.UserID == userID {
			return req.Code
		}
	}

	// Generate a new 6-digit random code
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	code := fmt.Sprintf("AUTH-%06d", r.Intn(1000000))

	m.pendingAuths[code] = PendingRequest{
		Code:      code,
		UserID:    userID,
		Username:  username,
		ChannelID: channelID,
	}

	log.Printf("Generated authorization code %s for user %s (%s)\n", code, username, userID)
	return code
}

// ApproveRequest whitelists a user by code, updating config.json and removing them from pending list.
func (m *Manager) ApproveRequest(code string) (string, error) {
	m.mu.Lock()
	req, found := m.pendingAuths[code]
	if !found {
		// Try to look up by UserID directly in case of WebUI approval by User ID
		for c, r := range m.pendingAuths {
			if r.UserID == code || r.Code == code {
				req = r
				code = c
				found = true
				break
			}
		}
	}
	m.mu.Unlock() // unlock to allow config Load/Save which are thread safe on disk

	if !found {
		return "", fmt.Errorf("authorization code or user ID not found")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if already whitelisted
	alreadyWhitelisted := false
	for _, u := range cfg.Discord.Whitelist {
		if u == req.UserID {
			alreadyWhitelisted = true
			break
		}
	}

	if !alreadyWhitelisted {
		cfg.Discord.Whitelist = append(cfg.Discord.Whitelist, req.UserID)
		if err := config.Save(cfg); err != nil {
			return "", fmt.Errorf("failed to save whitelist config: %w", err)
		}
	}

	// Remove from pending list
	m.mu.Lock()
	delete(m.pendingAuths, code)
	m.mu.Unlock()

	log.Printf("Successfully approved user %s (%s). Added to whitelist.\n", req.Username, req.UserID)
	return req.UserID, nil
}

// RemoveWhitelistedUser removes a user ID from the whitelist in config.json.
func (m *Manager) RemoveWhitelistedUser(userID string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	var newWhitelist []string
	found := false
	for _, u := range cfg.Discord.Whitelist {
		if u == userID {
			found = true
			continue
		}
		newWhitelist = append(newWhitelist, u)
	}

	if !found {
		return fmt.Errorf("user ID %s not found in whitelist", userID)
	}

	cfg.Discord.Whitelist = newWhitelist
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save whitelist config: %w", err)
	}

	log.Printf("Successfully removed user ID %s from whitelist.\n", userID)
	return nil
}
