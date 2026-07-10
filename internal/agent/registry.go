package agent

import (
	"botson/internal/tools"
	"sort"

	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/loadartifactstool"
)

// toolBuilder defines a builder function signature for ADK tools.
type toolBuilder func() (tool.Tool, error)

// confirmationGated is the single source of truth for which registry tools
// are built with RequireConfirmation: true. The tool configs below read from
// it, and internal/toolorder consults it (via RequiresConfirmation) to know
// which calls will pause for HITL approval on their own versus need a
// synthetic ordering confirmation -- keep the two in sync by construction,
// not by hand.
var confirmationGated = map[string]bool{
	"saveArtifact":   true,
	"updateSettings": true,
	"writeFile":      true,
	"editFile":       true,
	"runCommand":     true,
}

// RequiresConfirmation reports whether the named registry tool pauses for a
// human-in-the-loop confirmation before running.
func RequiresConfirmation(toolName string) bool {
	return confirmationGated[toolName]
}

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
			RequireConfirmation: confirmationGated["saveArtifact"],
		}, tools.SaveArtifact)
	},
	"updateSettings": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "updateSettings",
			Description:         "Changes Botson's own non-secret settings (Gemini model, root agent) and persists them immediately. Cannot touch API keys.",
			RequireConfirmation: confirmationGated["updateSettings"],
		}, tools.UpdateSettings)
	},
	"writeFile": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "writeFile",
			Description:         "Writes (creates or overwrites) a file at the given path within the project workspace, creating parent directories as needed. An existing file must be read via readFile first; new files don't need this.",
			RequireConfirmation: confirmationGated["writeFile"],
		}, tools.WriteFile)
	},
	"editFile": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "editFile",
			Description:         "Makes a precise find-and-replace edit to an existing file within the project workspace: oldString must match the file's current content exactly and uniquely (unless replaceAll is set). The file must have been read via readFile earlier in this conversation.",
			RequireConfirmation: confirmationGated["editFile"],
		}, tools.EditFile)
	},
	"runCommand": func() (tool.Tool, error) {
		return functiontool.New(functiontool.Config{
			Name:                "runCommand",
			Description:         "Runs a shell command in the project workspace and returns its stdout, stderr, and exit code. Use for builds, tests, git, and other CLI operations.",
			RequireConfirmation: confirmationGated["runCommand"],
		}, tools.RunCommand)
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
