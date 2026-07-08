package tools

import (
	"botsonv2/core/scripts"

	"google.golang.org/adk/v2/agent"
)

// SaveScriptArgs defines the input arguments for the Save Script tool.
type SaveScriptArgs struct {
	Name        string `json:"name" jsonschema:"A short name for the script (alphanumeric, underscores, and dashes only). Saving an existing name overwrites it."`
	Description string `json:"description,omitempty" jsonschema:"A short human-readable description of what the script does."`
	Source      string `json:"source" jsonschema:"The full contents of the script's main.go, a complete 'package main' Go program."`
}

// SaveScript lets the agent write its own named script -- a Go program
// saved to ~/.botsonv2/scripts/<name>/main.go -- that it (via runScript)
// or the user (via `botson script run`) can invoke by name afterward,
// instead of the agent re-writing equivalent code inline every time.
func SaveScript(ctx agent.Context, args SaveScriptArgs) (string, error) {
	if err := scripts.Save(scripts.Detail{
		Name:        args.Name,
		Description: args.Description,
		Source:      args.Source,
	}); err != nil {
		return "", err
	}
	return "Saved script " + args.Name, nil
}
