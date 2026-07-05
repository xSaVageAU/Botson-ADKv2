package agent

import (
	"botsonv2/core/tools"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

func NewAssistantAgent(model model.LLM) (agent.Agent, error) {
	listFilesTool, err := functiontool.New(functiontool.Config{
		Name:        "listFiles",
		Description: "Lists files and folders within the current workspace or relative subdirectories to help examine project structure.",
	}, tools.ListFiles)
	if err != nil {
		return nil, err
	}

	readFileTool, err := functiontool.New(functiontool.Config{
		Name:        "readFile",
		Description: "Reads the content of a file given its path.",
	}, tools.ReadFile)
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "general_assistant",
		Model:       model,
		Description: "A general-purpose AI assistant designed to help with development tasks and project management.",
		Instruction: `You are a General Assistant.
You have access to two tools:
1. 'listFiles': Use this to see what files exist in the user's workspace.
2. 'readFile': Use this to read the content of a specific file provided by the user.

Always be technical, polite, and direct.`,
		Tools: []tool.Tool{
			listFilesTool,
			readFileTool,
		},
	})
}
