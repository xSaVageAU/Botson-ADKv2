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
	Private     bool     `json:"private"`
	Tools       []string `json:"tools"`
}

// LoadedAgent wraps the initialized agent and its loading configurations.
type LoadedAgent struct {
	Agent   adkagent.Agent
	IsRoot  bool
	Private bool
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
		Agent:   createdAgent,
		IsRoot:  cfg.IsRoot,
		Private: cfg.Private,
	}

	// Cache the fully built agent
	built[name] = loaded
	return loaded, nil
}

// readConfigsFromFS reads configurations and instructions from a filesystem and loads them into the provided maps.
func readConfigsFromFS(sysFS fs.FS, configs map[string]*AgentConfig, instructions map[string]string) error {
	entries, err := fs.ReadDir(sysFS, ".")
	if err != nil {
		return nil // Ignore non-existent or unreadable folder (e.g. empty user folder)
	}

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
			return fmt.Errorf("failed to parse config for %s: %w", agentDirName, err)
		}

		// Read instructions.md (optional)
		instructionsPath := agentDirName + "/instructions.md"
		if instBytes, err := fs.ReadFile(sysFS, instructionsPath); err == nil {
			instructions[cfg.Name] = string(instBytes)
		}

		configs[cfg.Name] = &cfg
	}

	return nil
}

// LoadAllAgents loads embedded default agents and user agents from ~/.botsonv2/agents/
func LoadAllAgents(embeddedFS fs.FS, model model.LLM) (adkagent.Loader, error) {
	configs := make(map[string]*AgentConfig)
	instructions := make(map[string]string)

	// 1. Read embedded agents
	if err := readConfigsFromFS(embeddedFS, configs, instructions); err != nil {
		return nil, fmt.Errorf("failed to load embedded configurations: %w", err)
	}

	// 2. Read custom user agents from disk (overriding embedded ones if they share the same Name)
	dataDir, err := GetDataDir()
	if err == nil {
		userFS := os.DirFS(dataDir)
		if err := readConfigsFromFS(userFS, configs, instructions); err != nil {
			return nil, fmt.Errorf("failed to load user configurations: %w", err)
		}
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no agents were found in either embedded or user directories")
	}

	// 3. Build all agents recursively over the combined configurations
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

	// 4. Find root agent and compile otherAgents list (filtering out private agents)
	var rootAgent adkagent.Agent
	var otherAgents []adkagent.Agent

	// Look for explicitly marked is_root
	for _, loaded := range built {
		if loaded.IsRoot {
			rootAgent = loaded.Agent
		} else if !loaded.Private {
			otherAgents = append(otherAgents, loaded.Agent)
		}
	}

	// If no root is explicitly marked, fall back to the first non-private agent
	if rootAgent == nil {
		for _, loaded := range built {
			if !loaded.Private {
				rootAgent = loaded.Agent
				break
			}
		}
		// If still nil, fall back to the first agent regardless of privacy
		if rootAgent == nil {
			for _, loaded := range built {
				rootAgent = loaded.Agent
				break
			}
		}
		// Rebuild otherAgents list excluding the root and private agents
		otherAgents = nil
		for _, loaded := range built {
			if loaded.Agent != rootAgent && !loaded.Private {
				otherAgents = append(otherAgents, loaded.Agent)
			}
		}
	}

	return adkagent.NewMultiLoader(rootAgent, otherAgents...)
}
