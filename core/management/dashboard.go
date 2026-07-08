package management

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"botsonv2/core/config"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
)

// AgentStat summarizes a loaded agent for dashboard display.
type AgentStat struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsRoot       bool   `json:"isRoot"`
	SessionCount int    `json:"sessionCount"`
}

// SessionStat summarizes a single chat session for dashboard display.
type SessionStat struct {
	ID             string `json:"id"`
	AgentName      string `json:"agentName"`
	UserID         string `json:"userId"`
	DisplayName    string `json:"displayName"`
	LastUpdateTime int64  `json:"lastUpdateTime"`
	EventCount     int    `json:"eventCount"`
}

// toSessionStat extracts the summary fields dashboard/CLI display both
// need, including __session_metadata__.displayName if the session has one
// -- shared so this extraction logic lives in exactly one place.
func toSessionStat(s session.Session) SessionStat {
	displayName := ""
	if val, err := s.State().Get("__session_metadata__"); err == nil {
		if metadataMap, ok := val.(map[string]any); ok {
			if dn, ok := metadataMap["displayName"].(string); ok {
				displayName = dn
			}
		}
	}

	return SessionStat{
		ID:             s.ID(),
		AgentName:      s.AppName(),
		UserID:         s.UserID(),
		DisplayName:    displayName,
		LastUpdateTime: s.LastUpdateTime().Unix(),
		EventCount:     s.Events().Len(),
	}
}

// DashboardStats is the overall aggregated system snapshot.
type DashboardStats struct {
	TotalAgents    int           `json:"totalAgents"`
	TotalSessions  int           `json:"totalSessions"`
	TotalEvents    int           `json:"totalEvents"`
	DbPath         string        `json:"dbPath"`
	Agents         []AgentStat   `json:"agents"`
	RecentSessions []SessionStat `json:"recentSessions"`
}

// GetDashboardStats aggregates per-agent and per-session counts from the
// given launcher configuration.
func GetDashboardStats(ctx context.Context, configLauncher *launcher.Config) (*DashboardStats, error) {
	if configLauncher == nil || configLauncher.AgentLoader == nil {
		return nil, fmt.Errorf("stats not available in standalone mode")
	}

	dataDir, err := config.GetDataDir()
	dbPath := ""
	if err == nil {
		dbPath = filepath.Join(dataDir, "sessions.db")
	}

	agentNames := configLauncher.AgentLoader.ListAgents()
	totalAgents := len(agentNames)

	var allSessions []session.Session
	var agentStats []AgentStat

	rootAgentName := ""
	if configLauncher.AgentLoader.RootAgent() != nil {
		rootAgentName = configLauncher.AgentLoader.RootAgent().Name()
	}

	for _, name := range agentNames {
		description := ""
		ag, err := configLauncher.AgentLoader.LoadAgent(name)
		if err == nil && ag != nil {
			description = ag.Description()
		}

		sessionCount := 0
		listResponse, err := configLauncher.SessionService.List(ctx, &session.ListRequest{
			AppName: name,
			UserID:  "",
		})
		if err == nil && listResponse != nil {
			sessionCount = len(listResponse.Sessions)
			allSessions = append(allSessions, listResponse.Sessions...)
		}

		agentStats = append(agentStats, AgentStat{
			Name:         name,
			Description:  description,
			IsRoot:       name == rootAgentName,
			SessionCount: sessionCount,
		})
	}

	totalSessions := len(allSessions)
	totalEvents := 0
	var sessionStats []SessionStat

	for _, s := range allSessions {
		totalEvents += s.Events().Len()
		sessionStats = append(sessionStats, toSessionStat(s))
	}

	sort.Slice(sessionStats, func(i, j int) bool {
		return sessionStats[i].LastUpdateTime > sessionStats[j].LastUpdateTime
	})

	recentSessions := sessionStats
	if len(recentSessions) > 10 {
		recentSessions = recentSessions[:10]
	}

	return &DashboardStats{
		TotalAgents:    totalAgents,
		TotalSessions:  totalSessions,
		TotalEvents:    totalEvents,
		DbPath:         dbPath,
		Agents:         agentStats,
		RecentSessions: recentSessions,
	}, nil
}

// ListSessionUsers returns the sorted set of unique session UserIDs across
// all agents, always including the default "web" UI context even if no
// sessions exist under it yet.
func ListSessionUsers(ctx context.Context, configLauncher *launcher.Config) ([]string, error) {
	if configLauncher == nil || configLauncher.SessionService == nil || configLauncher.AgentLoader == nil {
		return []string{"web"}, nil
	}

	agentNames := configLauncher.AgentLoader.ListAgents()

	userMap := map[string]bool{"web": true}
	for _, name := range agentNames {
		listResponse, err := configLauncher.SessionService.List(ctx, &session.ListRequest{
			AppName: name,
		})
		if err == nil && listResponse != nil {
			for _, s := range listResponse.Sessions {
				if s.UserID() != "" {
					userMap[s.UserID()] = true
				}
			}
		}
	}

	var users []string
	for u := range userMap {
		users = append(users, u)
	}
	sort.Strings(users)
	return users, nil
}
