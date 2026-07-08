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
			Description: "Reads a file's content, line-numbered like `cat -n`, paginated with offset/limit for large files. Reading a file (any range) is required before writeFile/editFile can modify it.",
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
	"updateSettings": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "updateSettings",
			Description:         "Changes Botson's own non-secret settings (Gemini model, root agent, default command) and persists them immediately. Cannot touch API keys or Discord credentials.",
			RequireConfirmation: true,
		}, tools.UpdateSettings)
	},
	"writeFile": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "writeFile",
			Description:         "Writes (creates or overwrites) a file at the given path within the project workspace, creating parent directories as needed. An existing file must be read via readFile first; new files don't need this.",
			RequireConfirmation: true,
		}, tools.WriteFile)
	},
	"editFile": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "editFile",
			Description:         "Makes a precise find-and-replace edit to an existing file within the project workspace: oldString must match the file's current content exactly and uniquely (unless replaceAll is set). The file must have been read via readFile earlier in this conversation.",
			RequireConfirmation: true,
		}, tools.EditFile)
	},
	"runCommand": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "runCommand",
			Description:         "Runs a shell command in the project workspace and returns its stdout, stderr, and exit code. Use for builds, tests, git, and other CLI operations.",
			RequireConfirmation: true,
		}, tools.RunCommand)
	},
	"saveScript": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "saveScript",
			Description:         "Saves a named Go program to ~/.botsonv2/scripts/<name>/main.go, runnable later by name via runScript or `botson script run` -- for reusable automation instead of rewriting the same code inline every time.",
			RequireConfirmation: true,
		}, tools.SaveScript)
	},
	"runScript": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "runScript",
			Description:         "Runs a previously saved script by name (see saveScript or `botson script list`) and returns its stdout, stderr, and exit code.",
			RequireConfirmation: true,
		}, tools.RunScript)
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
