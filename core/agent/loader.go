package agent

import (
	"botsonv2/core/config"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	baseDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	dataDir := filepath.Join(baseDir, "agents")
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

	// 3. Resolve prompt placeholders
	resolvedPrompt := resolvePlaceholders(instructions[name])

	// 4. Create the LLM Agent
	createdAgent, err := llmagent.New(llmagent.Config{
		Name:        cfg.Name,
		Model:       model,
		Description: cfg.Description,
		Instruction: resolvedPrompt,
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
	appConfig, errCfg := config.Load()
	rootAgentName := "general_assistant"
	if errCfg == nil && appConfig.RootAgent != "" {
		rootAgentName = appConfig.RootAgent
	}

	var rootAgent adkagent.Agent
	var otherAgents []adkagent.Agent

	// Look for configured root agent
	if loaded, ok := built[rootAgentName]; ok {
		rootAgent = loaded.Agent
	}

	// Fallback if not found
	if rootAgent == nil {
		for _, loaded := range built {
			if !loaded.Private {
				rootAgent = loaded.Agent
				break
			}
		}
		if rootAgent == nil {
			for _, loaded := range built {
				rootAgent = loaded.Agent
				break
			}
		}
	}

	// Build otherAgents list excluding the resolved root
	for _, loaded := range built {
		if loaded.Agent != rootAgent && !loaded.Private {
			otherAgents = append(otherAgents, loaded.Agent)
		}
	}

	return adkagent.NewMultiLoader(rootAgent, otherAgents...)
}

// AgentDetail represents the configuration, instructions, and read-only status of an agent.
type AgentDetail struct {
	AgentConfig
	Instructions string `json:"instructions"`
	ReadOnly     bool   `json:"read_only"`
}

// GetAgentDetails returns raw details of all loaded agents from the embedded default filesystem and user home folder.
func GetAgentDetails(embeddedFS fs.FS) ([]AgentDetail, error) {
	configs := make(map[string]*AgentConfig)
	instructions := make(map[string]string)
	readOnly := make(map[string]bool)

	// 1. Read embedded configurations (marked as read-only)
	var embeddedConfigs = make(map[string]*AgentConfig)
	var embeddedInstructions = make(map[string]string)
	if err := readConfigsFromFS(embeddedFS, embeddedConfigs, embeddedInstructions); err == nil {
		for name, cfg := range embeddedConfigs {
			configs[name] = cfg
			instructions[name] = embeddedInstructions[name]
			readOnly[name] = true
		}
	}

	// 2. Read custom user configurations
	dataDir, err := GetDataDir()
	if err == nil {
		userFS := os.DirFS(dataDir)
		var userConfigs = make(map[string]*AgentConfig)
		var userInstructions = make(map[string]string)
		if err := readConfigsFromFS(userFS, userConfigs, userInstructions); err == nil {
			for name, cfg := range userConfigs {
				configs[name] = cfg
				instructions[name] = userInstructions[name]
				readOnly[name] = false // User configs override default configs and are not read-only
			}
		}
	}

	// 3. Assemble results list
	appConfig, _ := config.Load()
	rootAgentName := "general_assistant"
	if appConfig != nil && appConfig.RootAgent != "" {
		rootAgentName = appConfig.RootAgent
	}

	var details []AgentDetail
	for name, cfg := range configs {
		cfg.IsRoot = (name == rootAgentName) // Resolve root state dynamically
		details = append(details, AgentDetail{
			AgentConfig:  *cfg,
			Instructions: instructions[name],
			ReadOnly:     readOnly[name],
		})
	}

	return details, nil
}

// resolvePlaceholders evaluates and replaces dynamic tags inside agent instructions.
func resolvePlaceholders(prompt string) string {
	// 1. Resolve {{OS}}
	osName := runtime.GOOS
	switch osName {
	case "windows":
		osName = "Windows"
	case "darwin":
		osName = "macOS"
	case "linux":
		osName = "Linux"
	}
	prompt = strings.ReplaceAll(prompt, "{{OS}}", osName)

	// 2. Resolve {{DATE}}
	currentDate := time.Now().Format("January 2, 2006")
	prompt = strings.ReplaceAll(prompt, "{{DATE}}", currentDate)

	// 3. Resolve {{TIME}}
	currentTime := time.Now().Format("15:04:05 MST")
	prompt = strings.ReplaceAll(prompt, "{{TIME}}", currentTime)

	// 4. Resolve {{USER}}
	username := "User"
	if u, err := user.Current(); err == nil {
		username = u.Username
		if idx := strings.LastIndex(username, "\\"); idx != -1 {
			username = username[idx+1:]
		}
	} else if envUser := os.Getenv("USER"); envUser != "" {
		username = envUser
	} else if envUserWin := os.Getenv("USERNAME"); envUserWin != "" {
		username = envUserWin
	}
	prompt = strings.ReplaceAll(prompt, "{{USER}}", username)

	return prompt
}

