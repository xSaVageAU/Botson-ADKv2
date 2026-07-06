package webui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gorilla/mux"

	"botsonv2/core/agent"
	"google.golang.org/adk/v2/server/adkrest/controllers"
)

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_ -]+$`)

func registerBuilderRoutes(r *mux.Router) {
	// GET /botson/api/agents - returns list of all agents
	r.Methods("GET").Path("/agents").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subFS, err := fs.Sub(agent.GetDefaultAgentsFS(), "default_agents")
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to resolve default agents: %v", err), http.StatusInternalServerError)
			return
		}

		details, err := agent.GetAgentDetails(subFS)
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

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || !nameRegex.MatchString(req.Name) {
			http.Error(w, "invalid agent name: must contain only alphanumeric characters, spaces, underscores, and dashes", http.StatusBadRequest)
			return
		}

		subFS, err := fs.Sub(agent.GetDefaultAgentsFS(), "default_agents")
		if err == nil {
			defaultDetails, err := agent.GetAgentDetails(subFS)
			if err == nil {
				for _, d := range defaultDetails {
					if d.Name == req.Name && d.ReadOnly {
						req.ReadOnly = false
					}
				}
			}
		}

		dataDir, err := agent.GetDataDir()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to resolve data directory: %v", err), http.StatusInternalServerError)
			return
		}

		agentDir := filepath.Join(dataDir, req.Name)
		if err := os.MkdirAll(agentDir, 0755); err != nil {
			http.Error(w, fmt.Sprintf("failed to create agent directory: %v", err), http.StatusInternalServerError)
			return
		}

		configBytes, err := json.MarshalIndent(req.AgentConfig, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to serialize agent config: %v", err), http.StatusInternalServerError)
			return
		}

		err = os.WriteFile(filepath.Join(agentDir, "config.json"), configBytes, 0644)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to write config.json: %v", err), http.StatusInternalServerError)
			return
		}

		err = os.WriteFile(filepath.Join(agentDir, "instructions.md"), []byte(req.Instructions), 0644)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to write instructions.md: %v", err), http.StatusInternalServerError)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{"status": "success", "message": "Agent saved successfully"}, http.StatusOK, w)
	})

	// DELETE /botson/api/agents/{name} - deletes a custom agent
	r.Methods("DELETE").Path("/agents/{name}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		name := vars["name"]
		if name == "" || !nameRegex.MatchString(name) {
			http.Error(w, "invalid agent name", http.StatusBadRequest)
			return
		}

		dataDir, err := agent.GetDataDir()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to resolve data directory: %v", err), http.StatusInternalServerError)
			return
		}

		agentDir := filepath.Join(dataDir, name)

		if _, err := os.Stat(agentDir); os.IsNotExist(err) {
			http.Error(w, "agent not found or is a read-only default agent", http.StatusNotFound)
			return
		}

		if err := os.RemoveAll(agentDir); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete agent directory: %v", err), http.StatusInternalServerError)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{"status": "success", "message": "Agent deleted successfully"}, http.StatusOK, w)
	})

	// GET /botson/api/tools - returns list of standard tools + other agents (for delegation)
	r.Methods("GET").Path("/tools").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subFS, err := fs.Sub(agent.GetDefaultAgentsFS(), "default_agents")
		var agentNames []string
		if err == nil {
			details, err := agent.GetAgentDetails(subFS)
			if err == nil {
				for _, d := range details {
					agentNames = append(agentNames, d.Name)
				}
			}
		}

		response := map[string][]string{
			"standard": agent.GetAvailableTools(),
			"agents":   agentNames,
		}

		controllers.EncodeJSONResponse(response, http.StatusOK, w)
	})
}
