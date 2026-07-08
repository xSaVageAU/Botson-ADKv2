package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"botsonv2/internal/agent"
	"botsonv2/internal/management"
	"google.golang.org/adk/v2/server/adkrest/controllers"
)

func registerBuilderRoutes(r *mux.Router) {
	// GET /botson/api/agents - returns list of all agents
	r.Methods("GET").Path("/agents").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		details, err := management.ListAgents()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get agent details: %v", err), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(details, http.StatusOK, w)
	})

	// POST /botson/api/agents - saves a custom agent
	r.Methods("POST").Path("/agents").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req agent.AgentDetail
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON payload: %v", err), http.StatusBadRequest)
			return
		}

		if err := management.SaveAgent(req); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, management.ErrInvalidAgentName) {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{"status": "success", "message": "Agent saved successfully"}, http.StatusOK, w)
	})

	// DELETE /botson/api/agents/{name} - deletes a custom agent
	r.Methods("DELETE").Path("/agents/{name}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["name"]

		if err := management.DeleteAgent(name); err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, management.ErrInvalidAgentName):
				status = http.StatusBadRequest
			case errors.Is(err, management.ErrAgentNotFound):
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{"status": "success", "message": "Agent deleted successfully"}, http.StatusOK, w)
	})

	// GET /botson/api/tools - returns list of standard tools + other agents (for delegation)
	r.Methods("GET").Path("/tools").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tools, err := management.ListTools()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(tools, http.StatusOK, w)
	})
}
