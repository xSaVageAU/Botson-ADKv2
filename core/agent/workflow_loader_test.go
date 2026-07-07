package agent

import (
	"os"
	"testing"

	adkagent "google.golang.org/adk/v2/agent"
)


func TestCompileWorkflow(t *testing.T) {
	// Set mock config directory
	tmpDir, err := os.MkdirTemp("", "botson-workflow-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("BOTSON_DATA_DIR", tmpDir)

	// Build a mock environment of loaded agents
	mockAgent, _ := adkagent.New(adkagent.Config{
		Name:        "agent_botson",
		Description: "The default assistant.",
		Run:         nil,
	})

	built := map[string]LoadedAgent{
		"agent_botson": {
			Agent: mockAgent,
		},
	}

	cfg := &WorkflowConfig{
		Name:        "test_workflow",
		Description: "A simple pipeline workflow",
		Nodes: []NodeConfig{
			{ID: "start", Type: "start"},
			{ID: "coder", Type: "agent", AgentName: "agent_botson"},
		},
		Edges: []EdgeConfig{
			{From: "start", To: "coder"},
		},
	}

	compiled, err := CompileWorkflow(cfg, built)
	if err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	if compiled.Name() != "test_workflow" {
		t.Errorf("expected compiled agent name 'test_workflow', got '%s'", compiled.Name())
	}

	if compiled.Description() != "A simple pipeline workflow" {
		t.Errorf("expected description 'A simple pipeline workflow', got '%s'", compiled.Description())
	}
}

func TestWorkflowDiskOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "botson-disk-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	t.Setenv("BOTSON_DATA_DIR", tmpDir)

	cfg := &WorkflowConfig{
		Name:        "disk_workflow",
		Description: "Workflow saved to disk",
		Nodes: []NodeConfig{
			{ID: "start", Type: "start"},
		},
		Edges: []EdgeConfig{},
	}

	err = SaveWorkflowConfigToDisk(cfg)
	if err != nil {
		t.Fatalf("SaveWorkflowConfigToDisk failed: %v", err)
	}

	configs, err := ReadWorkflowConfigsFromDisk()
	if err != nil {
		t.Fatalf("ReadWorkflowConfigsFromDisk failed: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	if configs[0].Name != "disk_workflow" {
		t.Errorf("expected saved name 'disk_workflow', got '%s'", configs[0].Name)
	}

	err = DeleteWorkflowConfigFromDisk("disk_workflow")
	if err != nil {
		t.Fatalf("DeleteWorkflowConfigFromDisk failed: %v", err)
	}

	configs, err = ReadWorkflowConfigsFromDisk()
	if err != nil {
		t.Fatalf("ReadWorkflowConfigsFromDisk failed: %v", err)
	}

	if len(configs) != 0 {
		t.Errorf("expected 0 configs after deletion, got %d", len(configs))
	}
}
