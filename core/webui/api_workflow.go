package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"

	"botsonv2/core/agent"
	"google.golang.org/adk/v2/server/adkrest/controllers"
)

var workflowNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_ -]+$`)

// registerWorkflowRoutes registers CRUD endpoints for visual workflows.
func registerWorkflowRoutes(r *mux.Router) {
	// GET /botson/api/workflows - Returns list of all workflows
	r.Methods("GET").Path("/workflows").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configs, err := agent.ReadWorkflowConfigsFromDisk()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read workflows: %v", err), http.StatusInternalServerError)
			return
		}
		if configs == nil {
			configs = []agent.WorkflowConfig{}
		}
		controllers.EncodeJSONResponse(configs, http.StatusOK, w)
	})

	// POST /botson/api/workflows - Saves/overwrites a workflow configuration and reloads agents
	r.Methods("POST").Path("/workflows").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req agent.WorkflowConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON payload: %v", err), http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || !workflowNameRegex.MatchString(req.Name) {
			http.Error(w, "invalid workflow name: must contain only alphanumeric characters, spaces, underscores, and dashes", http.StatusBadRequest)
			return
		}

		// Save the workflow to disk
		if err := agent.SaveWorkflowConfigToDisk(&req); err != nil {
			http.Error(w, fmt.Sprintf("failed to save workflow: %v", err), http.StatusInternalServerError)
			return
		}

		// Hot-reload agents and workflows cache in the running process
		if err := agent.ReloadAgents(); err != nil {
			http.Error(w, fmt.Sprintf("workflow saved, but failed to hot-reload: %v", err), http.StatusInternalServerError)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{
			"status":  "success",
			"message": "Workflow saved and loaded successfully",
		}, http.StatusOK, w)
	})

	// DELETE /botson/api/workflows/{name} - Deletes a workflow and reloads agents
	r.Methods("DELETE").Path("/workflows/{name}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		name := strings.TrimSpace(vars["name"])

		if err := agent.DeleteWorkflowConfigFromDisk(name); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete workflow: %v", err), http.StatusInternalServerError)
			return
		}

		// Hot-reload to remove from running process cache
		if err := agent.ReloadAgents(); err != nil {
			http.Error(w, fmt.Sprintf("workflow deleted, but failed to hot-reload: %v", err), http.StatusInternalServerError)
			return
		}

		controllers.EncodeJSONResponse(map[string]string{
			"status":  "success",
			"message": "Workflow deleted and unloaded successfully",
		}, http.StatusOK, w)
	})
}
