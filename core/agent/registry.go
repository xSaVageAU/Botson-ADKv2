package agent

import (
	"botsonv2/core/tools"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// toolBuilder defines a builder function signature for ADK tools.
type toolBuilder func() (tool.Tool, error)

// availableTools registers Go functions to string identifiers.
var availableTools = map[string]toolBuilder{
	"listFiles": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:        "listFiles",
			Description: "Lists files and folders within the current workspace or relative subdirectories to help examine project structure.",
		}, tools.ListFiles)
	},
	"readFile": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:        "readFile",
			Description: "Reads the content of a file given its path.",
		}, tools.ReadFile)
	},
}
