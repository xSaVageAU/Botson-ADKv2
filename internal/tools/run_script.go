package tools

import (
	"botsonv2/internal/scripts"

	"google.golang.org/adk/v2/agent"
)

// RunScriptArgs defines the input arguments for the Run Script tool.
type RunScriptArgs struct {
	Name           string   `json:"name" jsonschema:"The name of a previously saved script to run (see the saveScript tool, or the botson script list CLI command)."`
	Args           []string `json:"args,omitempty" jsonschema:"Command-line arguments to pass to the script."`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty" jsonschema:"Maximum seconds to let the script run before it's killed. Defaults to 120."`
}

// RunScriptResult carries back everything the agent needs to judge
// whether the script succeeded.
type RunScriptResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// RunScript lets the agent run a previously saved script (a Go program
// under ~/.botsonv2/scripts/<name>/main.go) by name via `go run`, the same
// mechanism `botson script run` uses.
func RunScript(ctx agent.Context, args RunScriptArgs) (RunScriptResult, error) {
	result, err := scripts.Run(ctx, args.Name, args.Args, args.TimeoutSeconds)
	if err != nil {
		return RunScriptResult{}, err
	}

	return RunScriptResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}, nil
}
