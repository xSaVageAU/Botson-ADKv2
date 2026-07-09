package tools

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"botson/internal/procutil"

	"google.golang.org/adk/v2/agent"
)

// RunCommandArgs defines the input arguments for the Run Command tool.
type RunCommandArgs struct {
	Command        string `json:"command" jsonschema:"The shell command to run, exactly as you'd type it in a terminal (e.g. 'go build ./...' or 'git status')."`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty" jsonschema:"Maximum seconds to let the command run before it's killed. Defaults to 120."`
}

// RunCommandResult carries back everything the agent needs to judge
// whether the command succeeded.
type RunCommandResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// RunCommand lets the agent execute an arbitrary shell command in the
// project workspace -- builds, tests, git, and anything else a CLI could
// do. Runs via the platform's own shell so pipes/redirects/&& work exactly
// as the agent would expect from writing a normal command line. Resolves
// the effective root here (needs agent.Context for session state) and
// delegates to runCommand, which takes a plain context.Context (agent.Context
// satisfies it via embedding) plus that resolved root, so the actual exec
// logic stays unit-testable without an ADK mock.
func RunCommand(ctx agent.Context, args RunCommandArgs) (RunCommandResult, error) {
	return runCommand(ctx, effectiveRoot(ctx), args)
}

func runCommand(ctx context.Context, root string, args RunCommandArgs) (RunCommandResult, error) {
	if strings.TrimSpace(args.Command) == "" {
		return RunCommandResult{}, fmt.Errorf("command must not be empty")
	}

	shell, shellFlag := "/bin/sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/C"
	}

	var timeout time.Duration
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}

	result, err := procutil.Run(ctx, shell, []string{shellFlag, args.Command}, procutil.RunOptions{
		Dir:     root,
		Timeout: timeout,
	})
	if err != nil {
		return RunCommandResult{}, err
	}

	return RunCommandResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}, nil
}
