package webui

import (
	"net/http"
	"path/filepath"
	"sort"

	"github.com/gorilla/mux"

	"botsonv2/core/config"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/server/adkrest/controllers"
	"google.golang.org/adk/v2/session"
)

// AgentStat matches JSON output structure for agents listing
type AgentStat struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsRoot       bool   `json:"isRoot"`
	SessionCount int    `json:"sessionCount"`
}

// SessionStat matches JSON output structure for sessions listing
type SessionStat struct {
	ID             string `json:"id"`
	AgentName      string `json:"agentName"`
	DisplayName    string `json:"displayName"`
	LastUpdateTime int64  `json:"lastUpdateTime"`
	EventCount     int    `json:"eventCount"`
}

// DashboardStats represents overall aggregated statistics
type DashboardStats struct {
	TotalAgents    int           `json:"totalAgents"`
	TotalSessions  int           `json:"totalSessions"`
	TotalEvents    int           `json:"totalEvents"`
	DbPath         string        `json:"dbPath"`
	Agents         []AgentStat   `json:"agents"`
	RecentSessions []SessionStat `json:"recentSessions"`
}

func registerDashboardRoutes(r *mux.Router, configLauncher *launcher.Config) {
	// GET /botson/api/stats - returns calculated system stats
	r.Methods("GET").Path("/stats").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if configLauncher == nil || configLauncher.AgentLoader == nil {
			http.Error(w, "Stats not available in standalone mode", http.StatusNotImplemented)
			return
		}
		ctx := r.Context()
		
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

		// Gather stats per agent
		for _, name := range agentNames {
			description := ""
			ag, err := configLauncher.AgentLoader.LoadAgent(name)
			if err == nil && ag != nil {
				description = ag.Description()
			}

			// Query sessions for this agent
			sessionCount := 0
			listResponse, err := configLauncher.SessionService.List(ctx, &session.ListRequest{
				AppName: name,
				UserID:  "user",
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

		// Count events and map sessions
		totalSessions := len(allSessions)
		totalEvents := 0
		var sessionStats []SessionStat

		for _, s := range allSessions {
			eventCount := s.Events().Len()
			totalEvents += eventCount

			displayName := ""
			if val, err := s.State().Get("__session_metadata__"); err == nil {
				if metadataMap, ok := val.(map[string]any); ok {
					if dn, ok := metadataMap["displayName"].(string); ok {
						displayName = dn
					}
				}
			}

			sessionStats = append(sessionStats, SessionStat{
				ID:             s.ID(),
				AgentName:      s.AppName(),
				DisplayName:    displayName,
				LastUpdateTime: s.LastUpdateTime().Unix(),
				EventCount:     eventCount,
			})
		}

		// Sort sessions by last update time descending
		sort.Slice(sessionStats, func(i, j int) bool {
			return sessionStats[i].LastUpdateTime > sessionStats[j].LastUpdateTime
		})

		// Limit recent sessions to top 10
		recentSessions := sessionStats
		if len(recentSessions) > 10 {
			recentSessions = recentSessions[:10]
		}

		dashboardResponse := DashboardStats{
			TotalAgents:    totalAgents,
			TotalSessions:  totalSessions,
			TotalEvents:    totalEvents,
			DbPath:         dbPath,
			Agents:         agentStats,
			RecentSessions: recentSessions,
		}

		controllers.EncodeJSONResponse(dashboardResponse, http.StatusOK, w)
	})
}
