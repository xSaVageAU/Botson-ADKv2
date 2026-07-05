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
	"google.golang.org/adk/v2/tool/agenttool"
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

// getOrCreateAgent recursively builds agents, resolving standard tool strings and sub-agent dependencies.
func getOrCreateAgent(
	name string,
	configs map[string]*AgentConfig,
	instructions map[string]string,
	built map[string]LoadedAgent,
	active map[string]bool,
	model model.LLM,
) (LoadedAgent, error) {
	// 1. If already built and cached, return it
	if la, ok := built[name]; ok {
		return la, nil
	}

	// 2. Prevent circular dependencies
	if active[name] {
		return LoadedAgent{}, fmt.Errorf("circular dependency detected for agent %s", name)
	}
	active[name] = true
	defer delete(active, name)

	cfg, ok := configs[name]
	if !ok {
		return LoadedAgent{}, fmt.Errorf("configuration not found for agent %s", name)
	}

	var agentTools []tool.Tool
	for _, tName := range cfg.Tools {
		// A. Resolve standard Go tools from registry
		if builder, ok := availableTools[tName]; ok {
			t, err := builder()
			if err != nil {
				return LoadedAgent{}, err
			}
			agentTools = append(agentTools, t)
			continue
		}

		// B. Resolve other custom agents as tools dynamically
		if _, isSubAgent := configs[tName]; isSubAgent {
			subLoaded, err := getOrCreateAgent(tName, configs, instructions, built, active, model)
			if err != nil {
				return LoadedAgent{}, err
			}
			agentTools = append(agentTools, agenttool.New(subLoaded.Agent, nil))
			continue
		}

		return LoadedAgent{}, fmt.Errorf("unknown tool or sub-agent %q in %s's config", tName, name)
	}

	// 3. Create the LLM Agent
	createdAgent, err := llmagent.New(llmagent.Config{
		Name:        cfg.Name,
		Model:       model,
		Description: cfg.Description,
		Instruction: instructions[name],
		Tools:       agentTools,
	})
	if err != nil {
		return LoadedAgent{}, fmt.Errorf("failed to build agent %s: %w", cfg.Name, err)
	}

	loaded := LoadedAgent{
		Agent:  createdAgent,
		IsRoot: cfg.IsRoot,
	}

	// Cache the fully built agent
	built[name] = loaded
	return loaded, nil
}

// LoadAgentsFromFS walks a filesystem and parses any agent config subdirectories.
func LoadAgentsFromFS(sysFS fs.FS, model model.LLM) (map[string]LoadedAgent, error) {
	entries, err := fs.ReadDir(sysFS, ".")
	if err != nil {
		// Return empty if directory is unreadable or empty
		return nil, nil
	}

	configs := make(map[string]*AgentConfig)
	instructions := make(map[string]string)

	// Pass 1: Scan and load configs and prompts
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
		instructionsPath := agentDirName + "/instructions.md"
		if instBytes, err := fs.ReadFile(sysFS, instructionsPath); err == nil {
			instructions[cfg.Name] = string(instBytes)
		}

		configs[cfg.Name] = &cfg
	}

	// Pass 2: Recursively build agents and resolve tools/sub-agent dependencies
	built := make(map[string]LoadedAgent)
	active := make(map[string]bool)

	for name := range configs {
		if _, ok := built[name]; ok {
			continue
		}
		_, err := getOrCreateAgent(name, configs, instructions, built, active, model)
		if err != nil {
			return nil, err
		}
	}

	return built, nil
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
