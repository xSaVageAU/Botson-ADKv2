package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// AgentConfig represents the JSON schema of an agent's configuration metadata.
type AgentConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsRoot      bool     `json:"is_root"`
	Tools       []string `json:"tools"`
}

// LoadedAgent wraps the initialized agent and its loading configurations.
type LoadedAgent struct {
	Agent  adkagent.Agent
	IsRoot bool
}

// GetDataDir resolves the physical path to ~/.botsonv2/agents/ and ensures it exists.
func GetDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home directory: %w", err)
	}
	dataDir := filepath.Join(home, ".botsonv2", "agents")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}
	return dataDir, nil
}

// LoadAgentsFromFS walks a filesystem and parses any agent config subdirectories.
func LoadAgentsFromFS(sysFS fs.FS, model model.LLM) (map[string]LoadedAgent, error) {
	entries, err := fs.ReadDir(sysFS, ".")
	if err != nil {
		// Return empty if directory is unreadable or empty
		return nil, nil
	}

	loaded := make(map[string]LoadedAgent)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDirName := entry.Name()

		// Read config.json
		configPath := agentDirName + "/config.json"
		configBytes, err := fs.ReadFile(sysFS, configPath)
		if err != nil {
			continue // Skip folder if config.json is missing or unreadable
		}

		var cfg AgentConfig
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config for %s: %w", agentDirName, err)
		}

		// Read instructions.md (optional)
		var instructions string
		instructionsPath := agentDirName + "/instructions.md"
		if instBytes, err := fs.ReadFile(sysFS, instructionsPath); err == nil {
			instructions = string(instBytes)
		}

		// Map tools
		var agentTools []tool.Tool
		for _, tName := range cfg.Tools {
			builder, ok := availableTools[tName]
			if !ok {
				return nil, fmt.Errorf("unknown tool %s for agent %s", tName, cfg.Name)
			}
			t, err := builder()
			if err != nil {
				return nil, fmt.Errorf("failed to build tool %s: %w", tName, err)
			}
			agentTools = append(agentTools, t)
		}

		// Create LLM Agent
		createdAgent, err := llmagent.New(llmagent.Config{
			Name:        cfg.Name,
			Model:       model,
			Description: cfg.Description,
			Instruction: instructions,
			Tools:       agentTools,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create agent %s: %w", cfg.Name, err)
		}

		loaded[cfg.Name] = LoadedAgent{
			Agent:  createdAgent,
			IsRoot: cfg.IsRoot,
		}
	}

	return loaded, nil
}

// LoadAllAgents loads embedded default agents and user agents from ~/.botsonv2/agents/
func LoadAllAgents(embeddedFS fs.FS, model model.LLM) (adkagent.Loader, error) {
	// 1. Load embedded agents
	embeddedAgents, err := LoadAgentsFromFS(embeddedFS, model)
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded agents: %w", err)
	}

	// 2. Load custom user agents from disk
	userAgents := make(map[string]LoadedAgent)
	dataDir, err := GetDataDir()
	if err == nil {
		userFS := os.DirFS(dataDir)
		if uAgents, err := LoadAgentsFromFS(userFS, model); err == nil {
			userAgents = uAgents
		}
	}

	// 3. Merge: userAgents override embeddedAgents
	merged := make(map[string]LoadedAgent)
	for name, la := range embeddedAgents {
		merged[name] = la
	}
	for name, la := range userAgents {
		merged[name] = la
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("no agents were loaded")
	}

	// 4. Find root agent
	var rootAgent adkagent.Agent
	var otherAgents []adkagent.Agent

	// Look for explicitly marked is_root
	for _, loaded := range merged {
		if loaded.IsRoot {
			rootAgent = loaded.Agent
		} else {
			otherAgents = append(otherAgents, loaded.Agent)
		}
	}

	// If no root is explicitly marked, fall back to the first agent in map
	if rootAgent == nil {
		for _, loaded := range merged {
			rootAgent = loaded.Agent
			break
		}
		// Rebuild otherAgents list excluding the root
		otherAgents = nil
		for _, loaded := range merged {
			if loaded.Agent != rootAgent {
				otherAgents = append(otherAgents, loaded.Agent)
			}
		}
	}

	return adkagent.NewMultiLoader(rootAgent, otherAgents...)
}
