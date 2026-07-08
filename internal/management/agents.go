package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"botsonv2/internal/agent"
)

var agentNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_ -]+$`)

// Sentinel errors so callers (HTTP handlers, future TUI screens) can map
// failures to the right response without string-matching error text.
var (
	ErrInvalidAgentName = errors.New("invalid agent name: must contain only alphanumeric characters, spaces, underscores, and dashes")
	ErrAgentNotFound    = errors.New("agent not found or is a read-only default agent")
)

// ListAgents returns the combined details of all embedded default agents and
// custom user agents from disk.
func ListAgents() ([]agent.AgentDetail, error) {
	subFS, err := fs.Sub(agent.GetDefaultAgentsFS(), "default_agents")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve default agents: %w", err)
	}
	return agent.GetAgentDetails(subFS)
}

// ListTools returns the standard tool registry plus the names of agents
// available for delegation (sub-agent-as-tool).
func ListTools() (map[string][]string, error) {
	details, err := ListAgents()
	if err != nil {
		return nil, err
	}

	var agentNames []string
	for _, d := range details {
		agentNames = append(agentNames, d.Name)
	}

	return map[string][]string{
		"standard": agent.GetAvailableTools(),
		"agents":   agentNames,
	}, nil
}

// SaveAgent validates and persists a custom agent's config.json and
// instructions.md to ~/.botsonv2/agents/<name>/. If the name collides with a
// read-only default agent, it is saved as a user override instead.
func SaveAgent(detail agent.AgentDetail) error {
	detail.Name = strings.TrimSpace(detail.Name)
	if detail.Name == "" || !agentNameRegex.MatchString(detail.Name) {
		return ErrInvalidAgentName
	}

	if defaults, err := ListAgents(); err == nil {
		for _, d := range defaults {
			if d.Name == detail.Name && d.ReadOnly {
				detail.ReadOnly = false
			}
		}
	}

	dataDir, err := agent.GetDataDir()
	if err != nil {
		return fmt.Errorf("failed to resolve data directory: %w", err)
	}

	agentDir := filepath.Join(dataDir, detail.Name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	configBytes, err := json.MarshalIndent(detail.AgentConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize agent config: %w", err)
	}

	if err := os.WriteFile(filepath.Join(agentDir, "config.json"), configBytes, 0644); err != nil {
		return fmt.Errorf("failed to write config.json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(agentDir, "instructions.md"), []byte(detail.Instructions), 0644); err != nil {
		return fmt.Errorf("failed to write instructions.md: %w", err)
	}

	return nil
}

// DeleteAgent removes a custom user agent directory. Read-only default agents
// cannot be deleted since they have no corresponding user directory.
func DeleteAgent(name string) error {
	if name == "" || !agentNameRegex.MatchString(name) {
		return ErrInvalidAgentName
	}

	dataDir, err := agent.GetDataDir()
	if err != nil {
		return fmt.Errorf("failed to resolve data directory: %w", err)
	}

	agentDir := filepath.Join(dataDir, name)
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		return ErrAgentNotFound
	}

	if err := os.RemoveAll(agentDir); err != nil {
		return fmt.Errorf("failed to delete agent directory: %w", err)
	}

	return nil
}
