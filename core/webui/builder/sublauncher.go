package builder

import (
	"botsonv2/core/agent"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"

	"google.golang.org/adk/v2/cmd/launcher"
	weblauncher "google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/server/adkrest/controllers"
)

type builderSublauncher struct {
	flags *flag.FlagSet
}

func (b *builderSublauncher) Keyword() string {
	return "builder"
}

func (b *builderSublauncher) Parse(args []string) ([]string, error) {
	err := b.flags.Parse(args)
	if err != nil || !b.flags.Parsed() {
		return nil, fmt.Errorf("failed to parse builder flags: %v", err)
	}
	return b.flags.Args(), nil
}

func (b *builderSublauncher) CommandLineSyntax() string {
	return ""
}

func (b *builderSublauncher) SimpleDescription() string {
	return "starts Botson Agent Registry / Builder UI"
}

func (b *builderSublauncher) SetupSubrouters(router *mux.Router, config *launcher.Config) error {
	pathPrefix := "/builder/"

	// Redirect /builder to /builder/
	router.Methods("GET").Path("/builder").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathPrefix, http.StatusFound)
	})

	rBuilder := router.Methods("GET").PathPrefix(pathPrefix).Subrouter()

	// Serve the main builder HTML
	rBuilder.Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := content.ReadFile("index.html")
		if err != nil {
			http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Serve static files (under /builder/static/...)
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		return fmt.Errorf("cannot prepare builder static files: %v", err)
	}
	rBuilder.PathPrefix("/static/").Handler(http.StripPrefix(pathPrefix+"static/", http.FileServer(http.FS(staticFS))))

	// Mount Builder API under /builder/api/
	rAPI := router.PathPrefix("/builder/api").Subrouter()

	// GET /builder/api/agents - returns list of all agents
	rAPI.Methods("GET").Path("/agents").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// POST /builder/api/agents - saves a custom agent
	rAPI.Methods("POST").Path("/agents").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// DELETE /builder/api/agents/{name} - deletes a custom agent
	rAPI.Methods("DELETE").Path("/agents/{name}").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// GET /builder/api/tools - returns list of standard tools + other agents (for delegation)
	rAPI.Methods("GET").Path("/tools").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	return nil
}

func (b *builderSublauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("      builder:  you can access Agent Builder UI using %s/builder/", webURL))
}

// NewSublauncher creates a new Sublauncher for Agent Builder.
func NewSublauncher() weblauncher.Sublauncher {
	return &builderSublauncher{
		flags: flag.NewFlagSet("builder", flag.ContinueOnError),
	}
}
