package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"iter"
	"log"
	"os"
	"path/filepath"

	"sync"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/workflow"
)

// sliceToolset defines a simple toolset wrapping a slice of tools.
type sliceToolset struct {
	name  string
	tools []tool.Tool
}

func (s *sliceToolset) Name() string { return s.name }
func (s *sliceToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	return s.tools, nil
}

func newSliceToolset(name string, tools []tool.Tool) tool.Toolset {
	return &sliceToolset{name: name, tools: tools}
}

// LoadAllWorkflows scans the workflows data directory, compiles the json configurations,
// wraps them in standard agents, and adds them to the built cache map.
func LoadAllWorkflows(built map[string]LoadedAgent, model model.LLM) error {
	dir, err := GetWorkflowsDir()
	if err != nil {
		return nil // Ignore if dir cannot be created or accessed
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		workflowName := entry.Name()

		// Read workflow.json
		configPath := filepath.Join(dir, workflowName, "workflow.json")
		configBytes, err := os.ReadFile(configPath)
		if err != nil {
			log.Printf("Workflow Warning: failed to read config for %s: %v", workflowName, err)
			continue
		}

		var cfg WorkflowConfig
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			log.Printf("Workflow Error: failed to parse json for %s: %v", workflowName, err)
			continue
		}

		// Compile the workflow
		wfAgent, err := CompileWorkflow(&cfg, built)
		if err != nil {
			log.Printf("Workflow Error: failed to compile %s: %v", workflowName, err)
			continue
		}

		// Cache the compiled workflow as a first-class agent!
		built[workflowName] = LoadedAgent{
			Agent:   wfAgent,
			IsRoot:  false, // Workflows cannot be entry points by default unless selected globally
			Private: false,
		}
	}

	return nil
}

// CompileWorkflow converts a WorkflowConfig graph into a runnable adkagent.Agent.
func CompileWorkflow(cfg *WorkflowConfig, built map[string]LoadedAgent) (adkagent.Agent, error) {
	nodeMap := make(map[string]workflow.Node)

	// 1. Build Nodes
	var subAgents []adkagent.Agent
	for _, n := range cfg.Nodes {
		switch n.Type {
		case "start":
			nodeMap[n.ID] = workflow.Start
		case "agent":
			child, ok := built[n.AgentName]
			if !ok {
				return nil, fmt.Errorf("node %s references unknown agent %s", n.ID, n.AgentName)
			}
			agentNode, err := workflow.NewAgentNode(child.Agent, workflow.NodeConfig{})
			if err != nil {
				return nil, fmt.Errorf("failed to create agent node %s: %w", n.ID, err)
			}
			nodeMap[n.ID] = agentNode
			subAgents = append(subAgents, child.Agent)
		case "tool":
			toolBuilder, ok := availableTools[n.ToolName]
			if !ok {
				return nil, fmt.Errorf("node %s references unknown tool %s", n.ID, n.ToolName)
			}
			loadedTool, err := toolBuilder()
			if err != nil {
				return nil, fmt.Errorf("failed to load tool %s: %w", n.ToolName, err)
			}
			toolNode, err := workflow.NewToolNode(loadedTool, workflow.NodeConfig{})
			if err != nil {
				return nil, fmt.Errorf("failed to create tool node %s: %w", n.ID, err)
			}
			nodeMap[n.ID] = toolNode
		default:
			return nil, fmt.Errorf("unknown node type %s for node %s", n.Type, n.ID)
		}
	}

	// 2. Build Edges
	var edges []workflow.Edge
	for _, e := range cfg.Edges {
		fromNode, ok := nodeMap[e.From]
		if !ok {
			return nil, fmt.Errorf("edge references unknown from node %s", e.From)
		}
		toNode, ok := nodeMap[e.To]
		if !ok {
			return nil, fmt.Errorf("edge references unknown to node %s", e.To)
		}

		var route workflow.Route = workflow.Default
		if e.Route != "" && e.Route != "default" {
			route = workflow.StringRoute(e.Route)
		}

		edges = append(edges, workflow.Edge{
			From:  fromNode,
			To:    toNode,
			Route: route,
		})
	}

	// 3. Instantiate ADK Workflow
	w, err := workflow.New(cfg.Name, edges)
	if err != nil {
		return nil, fmt.Errorf("failed to compile workflow graph: %w", err)
	}

	// 4. Wrap workflow in standard Agent interface using agent.New
	wrappedAgent, err := adkagent.New(adkagent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   subAgents,
		Run: func(ctx adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
			return w.Run(ctx)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wrap workflow as agent: %w", err)
	}

	return wrappedAgent, nil
}

// ReadWorkflowConfigsFromDisk lists raw JSON config contents of all workflows saved in ~/.botsonv2/workflows/
func ReadWorkflowConfigsFromDisk() ([]WorkflowConfig, error) {
	dir, err := GetWorkflowsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	var results []WorkflowConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		wfName := entry.Name()
		configPath := filepath.Join(dir, wfName, "workflow.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}
		var cfg WorkflowConfig
		if err := json.Unmarshal(data, &cfg); err == nil {
			results = append(results, cfg)
		}
	}

	return results, nil
}

// SaveWorkflowConfigToDisk writes a WorkflowConfig as a JSON file to disk under ~/.botsonv2/workflows/<name>/workflow.json
func SaveWorkflowConfigToDisk(cfg *WorkflowConfig) error {
	dir, err := GetWorkflowsDir()
	if err != nil {
		return err
	}

	wfDir := filepath.Join(dir, cfg.Name)
	if err := os.MkdirAll(wfDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	configPath := filepath.Join(wfDir, "workflow.json")
	return os.WriteFile(configPath, data, 0644)
}

// DeleteWorkflowConfigFromDisk removes the workflow directory from disk under ~/.botsonv2/workflows/<name>
func DeleteWorkflowConfigFromDisk(name string) error {
	dir, err := GetWorkflowsDir()
	if err != nil {
		return err
	}

	wfDir := filepath.Join(dir, name)
	return os.RemoveAll(wfDir)
}

// GetWorkflowDetailsFS acts as a mock loader for embedded default workflow templates (if any)
func GetWorkflowDetailsFS(embeddedFS fs.FS) ([]WorkflowConfig, error) {
	return nil, nil // Return empty default workflows initially
}

// DynamicLoader wraps a static adkagent.Loader and provides a thread-safe hot reload mechanism.
type DynamicLoader struct {
	mu         sync.RWMutex
	delegate   adkagent.Loader
	embeddedFS fs.FS
	model      model.LLM
}

// NewDynamicLoader instantiates a new DynamicLoader wrapping the target delegate.
func NewDynamicLoader(embeddedFS fs.FS, model model.LLM, delegate adkagent.Loader) *DynamicLoader {
	return &DynamicLoader{
		embeddedFS: embeddedFS,
		model:      model,
		delegate:   delegate,
	}
}

func (d *DynamicLoader) ListAgents() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.delegate.ListAgents()
}

func (d *DynamicLoader) LoadAgent(name string) (adkagent.Agent, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.delegate.LoadAgent(name)
}

func (d *DynamicLoader) RootAgent() adkagent.Agent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.delegate.RootAgent()
}

func (d *DynamicLoader) Reload() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	newDelegate, err := LoadAllAgents(d.embeddedFS, d.model)
	if err != nil {
		return err
	}
	d.delegate = newDelegate
	return nil
}
