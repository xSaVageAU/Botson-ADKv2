package agent

import (
	"botsonv2/core/tools"
	"sort"

	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/loadartifactstool"
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
	"loadArtifacts": func() (tool.Tool, error) {
		return loadartifactstool.New(), nil
	},
	"saveArtifact": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "saveArtifact",
			Description:         "Saves a text artifact in the current session (e.g. plans, logs, code, or structured documents).",
			RequireConfirmation: true,
		}, tools.SaveArtifact)
	},
}

// GetAvailableTools returns the sorted list of all registered tool names in the registry.
func GetAvailableTools() []string {
	var list []string
	for k := range availableTools {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}
