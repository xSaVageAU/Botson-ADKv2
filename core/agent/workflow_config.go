package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"botsonv2/core/config"
)

// WorkflowConfig represents the layout and routing metadata for a visual workflow.
type WorkflowConfig struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Nodes       []NodeConfig `json:"nodes"`
	Edges       []EdgeConfig `json:"edges"`
}

// NodeConfig represents a node in the workflow graph.
type NodeConfig struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "start", "agent", "tool"
	AgentName string `json:"agent_name,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
}

// EdgeConfig represents a connection between two nodes.
type EdgeConfig struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Route string `json:"route,omitempty"` // "default", "success", "failure", etc.
}

// GetWorkflowsDir resolves the physical path to ~/.botsonv2/workflows/ and ensures it exists.
func GetWorkflowsDir() (string, error) {
	baseDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(baseDir, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create workflows directory: %w", err)
	}
	return dir, nil
}
