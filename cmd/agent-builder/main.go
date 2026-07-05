package main

import (
	"botsonv2/core/agent"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed index.html static/*
var content embed.FS

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func main() {
	mux := http.NewServeMux()

	// Serves dashboard HTML and static files
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			data, err := content.ReadFile("index.html")
			if err != nil {
				http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
				return
			}
			w.Write(data)
			return
		}

		filePath := strings.TrimPrefix(r.URL.Path, "/")
		data, err := content.ReadFile(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(filePath, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if strings.HasSuffix(filePath, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}
		w.Write(data)
	})

	// GET /api/agents - returns list of all agents
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(details)
	})

	// POST /api/agents - saves a custom agent
	mux.HandleFunc("POST /api/agents", func(w http.ResponseWriter, r *http.Request) {
		var req agent.AgentDetail
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON payload: %v", err), http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || !nameRegex.MatchString(req.Name) {
			http.Error(w, "invalid agent name: must contain only alphanumeric characters, underscores, and dashes", http.StatusBadRequest)
			return
		}

		// Prevent modifying/writing to protected embedded paths (though home dir write is separate, block overwriting protected configs directly if the UI sends it)
		subFS, err := fs.Sub(agent.GetDefaultAgentsFS(), "default_agents")
		if err == nil {
			defaultDetails, err := agent.GetAgentDetails(subFS)
			if err == nil {
				for _, d := range defaultDetails {
					if d.Name == req.Name && d.ReadOnly {
						// Block modification of read-only agents unless they want to save under a custom override
						// Actually, saving it in ~/.botsonv2/agents will override it. So let's allow it as a custom override!
						// But verify they are not trying to label it as read-only.
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

		// Write config.json
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

		// Write instructions.md
		err = os.WriteFile(filepath.Join(agentDir, "instructions.md"), []byte(req.Instructions), 0644)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to write instructions.md: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Agent saved successfully"})
	})

	// DELETE /api/agents/{name} - deletes a custom agent
	mux.HandleFunc("DELETE /api/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
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

		// Check if it exists before deleting
		if _, err := os.Stat(agentDir); os.IsNotExist(err) {
			http.Error(w, "agent not found or is a read-only default agent", http.StatusNotFound)
			return
		}

		if err := os.RemoveAll(agentDir); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete agent directory: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Agent deleted successfully"})
	})

	// GET /api/tools - returns list of standard tools + other agents (for delegation)
	mux.HandleFunc("GET /api/tools", func(w http.ResponseWriter, r *http.Request) {
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
			"standard": {"listFiles", "readFile"},
			"agents":   agentNames,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	port := ":8081"
	log.Printf("Starting Agent Builder server on http://localhost%s\n", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
