package dashboard

import (
	"botsonv2/core/config"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"

	"github.com/gorilla/mux"

	"google.golang.org/adk/v2/cmd/launcher"
	weblauncher "google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/server/adkrest/controllers"
	"google.golang.org/adk/v2/session"
)

//go:embed index.html static/*
var content embed.FS

type dashboardSublauncher struct {
	flags *flag.FlagSet
}

func (d *dashboardSublauncher) Keyword() string {
	return "dashboard"
}

func (d *dashboardSublauncher) Parse(args []string) ([]string, error) {
	err := d.flags.Parse(args)
	if err != nil || !d.flags.Parsed() {
		return nil, fmt.Errorf("failed to parse dashboard flags: %v", err)
	}
	return d.flags.Args(), nil
}

func (d *dashboardSublauncher) CommandLineSyntax() string {
	return ""
}

func (d *dashboardSublauncher) SimpleDescription() string {
	return "starts Botson Workspace Dashboard Interface"
}

func (d *dashboardSublauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("    dashboard:  you can access Workspace Dashboard using %s/dashboard/", webURL))
}

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

func (d *dashboardSublauncher) SetupSubrouters(router *mux.Router, configLauncher *launcher.Config) error {
	pathPrefix := "/dashboard/"

	rDashboard := router.Methods("GET").PathPrefix(pathPrefix).Subrouter()

	// Redirect /dashboard to /dashboard/
	router.Methods("GET").Path("/dashboard").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathPrefix, http.StatusFound)
	})

	// Redirect root / to /dashboard/
	router.Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathPrefix, http.StatusFound)
	})

	// Serve the main dashboard HTML
	rDashboard.Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := content.ReadFile("index.html")
		if err != nil {
			http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Serve static files (under /dashboard/static/...)
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		return fmt.Errorf("cannot prepare dashboard static files: %v", err)
	}
	rDashboard.PathPrefix("/static/").Handler(http.StripPrefix(pathPrefix+"static/", http.FileServer(http.FS(staticFS))))

	// Mount Dashboard API under /dashboard/api/
	rAPI := router.PathPrefix("/dashboard/api").Subrouter()

	// GET /dashboard/api/stats - returns calculated system stats
	rAPI.Methods("GET").Path("/stats").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// Build response payload
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

	return nil
}

// NewSublauncher creates a new Sublauncher for the Workspace Dashboard.
func NewSublauncher() weblauncher.Sublauncher {
	return &dashboardSublauncher{
		flags: flag.NewFlagSet("dashboard", flag.ContinueOnError),
	}
}
