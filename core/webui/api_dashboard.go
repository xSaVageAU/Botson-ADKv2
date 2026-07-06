package webui

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"sort"

	"github.com/gorilla/mux"

	"botsonv2/core/config"
	"botsonv2/core/gateways/discord"
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

	// GET /botson/api/users - returns list of all unique user IDs across all sessions
	r.Methods("GET").Path("/users").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if configLauncher == nil || configLauncher.SessionService == nil {
			controllers.EncodeJSONResponse([]string{"user"}, http.StatusOK, w)
			return
		}
		
		ctx := r.Context()
		agentNames := configLauncher.AgentLoader.ListAgents()
		
		userMap := make(map[string]bool)
		// Default UI context
		userMap["user"] = true

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

		controllers.EncodeJSONResponse(users, http.StatusOK, w)
	})

	// GET /botson/api/config - returns application configuration masked
	r.Methods("GET").Path("/config").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := config.Load()
		if err != nil {
			http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Clone and mask secrets
		clientCfg := *cfg
		if clientCfg.GeminiAPIKey != "" {
			clientCfg.GeminiAPIKey = "******"
		}
		if clientCfg.Discord.Token != "" {
			clientCfg.Discord.Token = "******"
		}

		controllers.EncodeJSONResponse(clientCfg, http.StatusOK, w)
	})

	// POST /botson/api/config - updates application configuration
	r.Methods("POST").Path("/config").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req config.AppConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Load current configuration from disk to merge secrets
		diskCfg, err := config.Load()
		if err != nil {
			http.Error(w, "Failed to load existing config: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Merge secret: if the key/token in request is "******", retain the original
		if req.GeminiAPIKey == "******" {
			req.GeminiAPIKey = diskCfg.GeminiAPIKey
		}
		if req.Discord.Token == "******" {
			req.Discord.Token = diskCfg.Discord.Token
		}

		// Save merged config to disk
		if err := config.Save(&req); err != nil {
			http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Trigger background Discord bot restart dynamically
		mgr := discord.GetManager()
		if mgr != nil {
			go func() {
				err := mgr.Restart(req.Discord.Enabled, req.Discord.Token)
				if err != nil {
					log.Printf("Dynamic Discord restart error: %v\n", err)
				}
			}()
		}

		controllers.EncodeJSONResponse(map[string]string{"status": "success", "message": "Settings saved successfully"}, http.StatusOK, w)
	})

	// GET /botson/api/discord/pending - lists all pending authorization requests
	r.Methods("GET").Path("/discord/pending").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr := discord.GetManager()
		if mgr == nil {
			controllers.EncodeJSONResponse([]discord.PendingRequest{}, http.StatusOK, w)
			return
		}
		controllers.EncodeJSONResponse(mgr.GetPendingRequests(), http.StatusOK, w)
	})

	// POST /botson/api/discord/approve - approves a pending user by code or ID
	r.Methods("POST").Path("/discord/approve").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if req.Code == "" {
			http.Error(w, "Code or UserID is required", http.StatusBadRequest)
			return
		}

		mgr := discord.GetManager()
		if mgr == nil {
			http.Error(w, "Discord manager not initialized", http.StatusInternalServerError)
			return
		}

		approvedUserID, err := mgr.ApproveRequest(req.Code)
		if err != nil {
			http.Error(w, "Approval failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{
			"status":  "success",
			"message": "User whitelisted successfully",
			"user_id": approvedUserID,
		}, http.StatusOK, w)
	})

	// POST /botson/api/discord/remove-whitelisted - removes a user from whitelist
	r.Methods("POST").Path("/discord/remove-whitelisted").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if req.UserID == "" {
			http.Error(w, "UserID is required", http.StatusBadRequest)
			return
		}

		mgr := discord.GetManager()
		if mgr == nil {
			http.Error(w, "Discord manager not initialized", http.StatusInternalServerError)
			return
		}

		if err := mgr.RemoveWhitelistedUser(req.UserID); err != nil {
			http.Error(w, "Removal failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{
			"status":  "success",
			"message": "User removed from whitelist successfully",
		}, http.StatusOK, w)
	})
}
