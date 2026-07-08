// Package procutil runs external commands with a timeout that actually
// works, and captures their output safely. It exists because
// exec.CommandContext's cancellation only kills the direct child process:
// a shell command that forks rather than exec-replaces itself (e.g.
// `sh -c "sleep 5"` on some platforms) would otherwise keep running past
// its parent's death, holding the captured stdout/stderr pipe open until
// it exits on its own -- silently defeating the timeout. This is the one
// place that fix lives, shared by every caller that shells out
// (core/tools' runCommand, core/scripts' script runner) instead of each
// reimplementing (and re-debugging) it.
package procutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// RunOptions configures a Run call. Zero values fall back to sane
// defaults (see Run).
type RunOptions struct {
	Dir            string        // working directory; empty means the caller's own cwd
	Timeout        time.Duration // <= 0 means DefaultTimeout
	MaxOutputBytes int           // <= 0 means DefaultMaxOutputBytes
}

// Result carries back everything a caller needs to judge whether the
// command succeeded.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

const (
	// DefaultTimeout is used when RunOptions.Timeout is <= 0.
	DefaultTimeout = 120 * time.Second
	// DefaultMaxOutputBytes guards against a runaway command (e.g. an
	// accidental infinite-output loop) blowing up the caller's context.
	DefaultMaxOutputBytes = 200_000
)

// Run executes name with args as a subprocess, capturing stdout/stderr,
// killing the whole process group (not just the direct child Go tracks --
// see the package doc) if it exceeds opts.Timeout, and truncating
// captured output past opts.MaxOutputBytes.
func Run(ctx context.Context, name string, args []string, opts RunOptions) (Result, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxOutput := opts.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = DefaultMaxOutputBytes
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = opts.Dir
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
		// *exec.ExitError (e.g. "signal: killed"), so without this
		// ordering a timeout would be silently reported as a normal exit.
		case runCtx.Err() == context.DeadlineExceeded:
			return Result{}, fmt.Errorf("command timed out after %s", timeout)
		case errors.As(runErr, &exitErr):
			exitCode = exitErr.ExitCode()
		default:
			return Result{}, fmt.Errorf("failed to run command: %w", runErr)
		}
	}

	return Result{
		Stdout:   truncate(stdout.String(), maxOutput),
		Stderr:   truncate(stderr.String(), maxOutput),
		ExitCode: exitCode,
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(s))
}
