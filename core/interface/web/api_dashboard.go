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
