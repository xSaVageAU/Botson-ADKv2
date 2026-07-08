package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"botsonv2/internal/config"
	"google.golang.org/adk/v2/session"
)

func (g *Gateway) saveActiveSessions() error {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}
	filePath := filepath.Join(dataDir, "discord_active_sessions.json")

	g.mu.RLock()
	data, err := json.MarshalIndent(g.activeSessions, "", "  ")
	g.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func (g *Gateway) loadActiveSessions() {
	dataDir, err := config.GetDataDir()
	if err != nil {
		log.Printf("Discord Warning: failed to resolve data directory: %v", err)
		return
	}
	filePath := filepath.Join(dataDir, "discord_active_sessions.json")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Discord Warning: failed to read active sessions file: %v", err)
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if err := json.Unmarshal(data, &g.activeSessions); err != nil {
		log.Printf("Discord Warning: failed to parse active sessions JSON: %v", err)
	} else {
		log.Printf("Discord Info: successfully loaded %d active sessions from disk.", len(g.activeSessions))
	}
}

// Helper: Resolve the active Session ID for the channel
func (g *Gateway) resolveSessionID(ctx context.Context, channelID string, agentName string) (string, error) {
	g.mu.RLock()
	sessionID, ok := g.activeSessions[channelID]
	g.mu.RUnlock()
	if ok && sessionID != "" {
		return sessionID, nil
	}

	// Look up in GORM DB for matching channel metadata
	sessions, err := g.getChannelSessions(ctx, channelID, agentName)
	if err == nil && len(sessions) > 0 {
		sessionID = sessions[0].ID()
		g.mu.Lock()
		g.activeSessions[channelID] = sessionID
		g.mu.Unlock()
		_ = g.saveActiveSessions()
		return sessionID, nil
	}

	// Create new session UUID if not found
	sessionID = uuid.New().String()
	_, err = g.config.SessionService.Create(ctx, &session.CreateRequest{
		AppName:   agentName,
		UserID:    "discord",
		SessionID: sessionID,
		State: map[string]any{
			"__session_metadata__": map[string]any{
				"displayName":        fmt.Sprintf("Discord - #%s", channelID),
				"discord_channel_id": channelID,
			},
		},
	})
	if err != nil {
		return "", err
	}

	g.mu.Lock()
	g.activeSessions[channelID] = sessionID
	g.mu.Unlock()
	_ = g.saveActiveSessions()

	return sessionID, nil
}

func (g *Gateway) getChannelSessions(ctx context.Context, channelID, agentName string) ([]session.Session, error) {
	listResp, err := g.config.SessionService.List(ctx, &session.ListRequest{
		AppName: agentName,
		UserID:  "discord",
	})
	if err != nil {
		return nil, err
	}

	var results []session.Session
	for _, sess := range listResp.Sessions {
		val, err := sess.State().Get("__session_metadata__")
		if err != nil {
			continue
		}
		var metaMap map[string]any
		
		// Handle potential database driver parsing variants
		switch v := val.(type) {
		case map[string]any:
			metaMap = v
		case string:
			json.Unmarshal([]byte(v), &metaMap)
		}

		if metaMap != nil {
			if chanID, ok := metaMap["discord_channel_id"].(string); ok && chanID == channelID {
				results = append(results, sess)
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].LastUpdateTime().After(results[j].LastUpdateTime())
	})

	return results, nil
}
