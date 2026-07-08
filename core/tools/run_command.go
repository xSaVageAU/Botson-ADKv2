package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

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

const (
	defaultCommandTimeout = 120 * time.Second
	// maxCommandOutputBytes guards against a runaway command (e.g. an
	// accidental infinite-output loop) blowing up the agent's context.
	maxCommandOutputBytes = 200_000
)

// RunCommand lets the agent execute an arbitrary shell command in the
// project workspace -- builds, tests, git, and anything else a CLI could
// do. Runs via the platform's own shell so pipes/redirects/&& work exactly
// as the agent would expect from writing a normal command line. Delegates
// to runCommand, which takes a plain context.Context (agent.Context
// satisfies it via embedding) so the actual exec logic is unit-testable
// without an ADK mock.
func RunCommand(ctx agent.Context, args RunCommandArgs) (RunCommandResult, error) {
	return runCommand(ctx, args)
}

func runCommand(ctx context.Context, args RunCommandArgs) (RunCommandResult, error) {
	if strings.TrimSpace(args.Command) == "" {
		return RunCommandResult{}, fmt.Errorf("command must not be empty")
	}

	timeout := defaultCommandTimeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	root, err := os.Getwd()
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("failed to get current working directory: %w", err)
	}

	shell, shellFlag := "/bin/sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/C"
	}

	cmd := exec.CommandContext(cmdCtx, shell, shellFlag, args.Command)
	cmd.Dir = root
	setNewProcessGroup(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		switch {
		// Checked before the generic *exec.ExitError case below: a process
		// killed because its deadline expired also surfaces as an
		// *exec.ExitError (e.g. "signal: killed"), so without this ordering
		// a timeout would be silently reported as a normal exit.
		case cmdCtx.Err() == context.DeadlineExceeded:
			return RunCommandResult{}, fmt.Errorf("command timed out after %s", timeout)
		case errors.As(runErr, &exitErr):
			exitCode = exitErr.ExitCode()
		default:
			return RunCommandResult{}, fmt.Errorf("failed to run command: %w", runErr)
		}
	}

	return RunCommandResult{
		Stdout:   truncateOutput(stdout.String()),
		Stderr:   truncateOutput(stderr.String()),
		ExitCode: exitCode,
	}, nil
}

func truncateOutput(s string) string {
	if len(s) <= maxCommandOutputBytes {
		return s
	}
	return s[:maxCommandOutputBytes] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(s))
}
