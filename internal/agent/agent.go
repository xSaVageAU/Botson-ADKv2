package agent

import (
	"embed"
	"io/fs"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
)

//go:embed default_agents/*
var defaultAgentsFS embed.FS

// LoadDefaultAgents initializes and returns the dynamic agent.Loader.
// It loads both the embedded default agents and any custom agents in ~/.botsonv2/agents/.
func LoadDefaultAgents(model model.LLM) (agent.Loader, error) {
	subFS, err := fs.Sub(defaultAgentsFS, "default_agents")
	if err != nil {
		return nil, err
	}
	return LoadAllAgents(subFS, model)
}

// GetDefaultAgentsFS returns the embedded default agents filesystem.
func GetDefaultAgentsFS() embed.FS {
	return defaultAgentsFS
}

