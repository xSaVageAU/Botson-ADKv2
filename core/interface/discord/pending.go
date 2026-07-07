package discord

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"botsonv2/core/config"
)

// PendingRequest is an unauthorized user's request for bot access, awaiting
// approval by the owner/an admin.
type PendingRequest struct {
	Code      string `json:"code"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	ChannelID string `json:"channel_id"`
}

// pendingMu guards read-modify-write access to the pending-auths file
// within this process. It does not protect against concurrent writes from
// another process (e.g. the gateway daemon and the web console running
// simultaneously) -- same accepted risk as config.Save's unlocked writes,
// since these are human-speed, one-at-a-time interactions.
var pendingMu sync.Mutex

// pendingAuthsPath persists requests to disk (rather than in memory) so
// they're visible across process boundaries: the Discord gateway daemon
// generates them, but the web console (a separate process) is what approves
// them.
func pendingAuthsPath() (string, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "discord_pending.json"), nil
}

func loadPendingAuths() (map[string]PendingRequest, error) {
	path, err := pendingAuthsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]PendingRequest{}, nil
		}
		return nil, err
	}
	var auths map[string]PendingRequest
	if err := json.Unmarshal(data, &auths); err != nil {
		return nil, err
	}
	if auths == nil {
		auths = map[string]PendingRequest{}
	}
	return auths, nil
}

func savePendingAuths(auths map[string]PendingRequest) error {
	path, err := pendingAuthsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(auths, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AddPendingRequest registers a pending user authentication request and
// returns its AUTH code (an existing code is reused if this user already
// has a pending request).
func AddPendingRequest(userID, username, channelID string) (string, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	auths, err := loadPendingAuths()
	if err != nil {
		return "", fmt.Errorf("failed to load pending authorizations: %w", err)
	}

	for _, req := range auths {
		if req.UserID == userID {
			return req.Code, nil
		}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	code := fmt.Sprintf("AUTH-%06d", r.Intn(1000000))

	auths[code] = PendingRequest{
		Code:      code,
		UserID:    userID,
		Username:  username,
		ChannelID: channelID,
	}

	if err := savePendingAuths(auths); err != nil {
		return "", fmt.Errorf("failed to save pending authorizations: %w", err)
	}

	log.Printf("Generated authorization code %s for user %s (%s)\n", code, username, userID)
	return code, nil
}

// GetPendingRequests returns the list of pending authorizations.
func GetPendingRequests() ([]PendingRequest, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	auths, err := loadPendingAuths()
	if err != nil {
		return nil, fmt.Errorf("failed to load pending authorizations: %w", err)
	}

	reqs := make([]PendingRequest, 0, len(auths))
	for _, r := range auths {
		reqs = append(reqs, r)
	}
	return reqs, nil
}

// ApproveRequest whitelists a user by code (or by user ID directly, for
// webui approvals), updating config.json and removing them from the
// pending list.
func ApproveRequest(code string) (string, error) {
	pendingMu.Lock()
	auths, err := loadPendingAuths()
	if err != nil {
		pendingMu.Unlock()
		return "", fmt.Errorf("failed to load pending authorizations: %w", err)
	}

	req, found := auths[code]
	if !found {
		for c, r := range auths {
			if r.UserID == code || r.Code == code {
				req = r
				code = c
				found = true
				break
			}
		}
	}
	pendingMu.Unlock()

	if !found {
		return "", fmt.Errorf("authorization code or user ID not found")
	}

	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load configuration: %w", err)
	}

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

	pendingMu.Lock()
	if auths, err := loadPendingAuths(); err == nil {
		delete(auths, code)
		_ = savePendingAuths(auths)
	}
	pendingMu.Unlock()

	log.Printf("Successfully approved user %s (%s). Added to whitelist.\n", req.Username, req.UserID)
	return req.UserID, nil
}

// RemoveWhitelistedUser removes a user ID from the whitelist in config.json.
func RemoveWhitelistedUser(userID string) error {
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
