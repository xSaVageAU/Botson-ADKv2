package web

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"botsonv2/core/config"
	"botsonv2/core/interface/discord"
	"botsonv2/core/management"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/server/adkrest/controllers"
)

func registerDashboardRoutes(r *mux.Router, configLauncher *launcher.Config) {
	// GET /botson/api/stats - returns calculated system stats
	r.Methods("GET").Path("/stats").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stats, err := management.GetDashboardStats(r.Context(), configLauncher)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotImplemented)
			return
		}
		controllers.EncodeJSONResponse(stats, http.StatusOK, w)
	})

	// GET /botson/api/users - returns list of all unique user IDs across all sessions
	r.Methods("GET").Path("/users").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		users, err := management.ListSessionUsers(r.Context(), configLauncher)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(users, http.StatusOK, w)
	})

	// GET /botson/api/config - returns application configuration masked
	r.Methods("GET").Path("/config").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := management.GetMaskedConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(cfg, http.StatusOK, w)
	})

	// POST /botson/api/config - updates application configuration
	r.Methods("POST").Path("/config").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req config.AppConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		if err := management.UpdateConfig(&req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{"status": "success", "message": "Settings saved successfully"}, http.StatusOK, w)
	})

	// GET /botson/api/default-agent - returns the configured root agent's
	// name, so a thin client (TUI, Discord) can learn which agent to talk
	// to without needing local AgentLoader.RootAgent() access -- the one
	// piece the ADK's own REST API (GET /api/list-apps, bare names only)
	// doesn't already cover.
	r.Methods("GET").Path("/default-agent").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if configLauncher == nil || configLauncher.AgentLoader == nil {
			http.Error(w, "no agent loader available", http.StatusServiceUnavailable)
			return
		}
		rootAgent := configLauncher.AgentLoader.RootAgent()
		if rootAgent == nil {
			http.Error(w, "no root agent loaded in this workspace", http.StatusNotFound)
			return
		}
		controllers.EncodeJSONResponse(map[string]string{"name": rootAgent.Name()}, http.StatusOK, w)
	})

	// GET /botson/api/discord/status - reports whether the background Discord gateway is running
	r.Methods("GET").Path("/discord/status").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, err := management.DiscordDaemonStatus()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(status, http.StatusOK, w)
	})

	// POST /botson/api/discord/start - starts the Discord gateway as a background daemon
	r.Methods("POST").Path("/discord/start").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := config.Load()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if cfg.Discord.Token == "" {
			http.Error(w, "Discord bot token is not configured", http.StatusBadRequest)
			return
		}

		pid, logPath, err := management.StartDiscordDaemon()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		controllers.EncodeJSONResponse(map[string]any{
			"status":  "success",
			"message": "Discord gateway started",
			"pid":     pid,
			"logPath": logPath,
		}, http.StatusOK, w)
	})

	// POST /botson/api/discord/stop - stops the background Discord gateway
	r.Methods("POST").Path("/discord/stop").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Force bool `json:"force"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req) // absent/empty body is fine, defaults to force=false

		if err := management.StopDiscordDaemon(req.Force); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{
			"status":  "success",
			"message": "Discord gateway stopped",
		}, http.StatusOK, w)
	})

	// GET /botson/api/discord/pending - lists all pending authorization requests
	r.Methods("GET").Path("/discord/pending").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pending, err := discord.GetPendingRequests()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(pending, http.StatusOK, w)
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

		approvedUserID, err := discord.ApproveRequest(req.Code)
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

		if err := discord.RemoveWhitelistedUser(req.UserID); err != nil {
			http.Error(w, "Removal failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{
			"status":  "success",
			"message": "User removed from whitelist successfully",
		}, http.StatusOK, w)
	})
}
