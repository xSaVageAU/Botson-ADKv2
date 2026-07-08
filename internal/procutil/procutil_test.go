package procutil

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func shellCommand(script string) (name string, args []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", script}
	}
	return "/bin/sh", []string{"-c", script}
}

func TestRun(t *testing.T) {
	t.Run("captures stdout and exit code 0", func(t *testing.T) {
		name, args := shellCommand("echo hello")
		result, err := Run(context.Background(), name, args, RunOptions{})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if !strings.Contains(result.Stdout, "hello") {
			t.Fatalf("expected stdout to contain %q, got %q", "hello", result.Stdout)
		}
		if result.ExitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", result.ExitCode)
		}
	})

	t.Run("captures non-zero exit code without erroring", func(t *testing.T) {
		name, args := shellCommand("exit 3")
		result, err := Run(context.Background(), name, args, RunOptions{})
		if err != nil {
			t.Fatalf("Run should report a non-zero exit via ExitCode, not err: %v", err)
		}
		if result.ExitCode != 3 {
			t.Fatalf("expected exit code 3, got %d", result.ExitCode)
		}
	})

	t.Run("kills a command that forks past its own timeout", func(t *testing.T) {
		sleepCmd := "sleep 5"
		if runtime.GOOS == "windows" {
			sleepCmd = "timeout /T 5"
		}
		name, args := shellCommand(sleepCmd)

		start := time.Now()
		_, err := Run(context.Background(), name, args, RunOptions{Timeout: 1 * time.Second})
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected a timeout error, got none")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected a timeout error, got: %v", err)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("Run took %s to return after a 1s timeout -- the forked child likely wasn't actually killed", elapsed)
		}
	})

	t.Run("truncates output past MaxOutputBytes", func(t *testing.T) {
		name, args := shellCommand("echo " + strings.Repeat("x", 50))
		result, err := Run(context.Background(), name, args, RunOptions{MaxOutputBytes: 10})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if !strings.Contains(result.Stdout, "truncated") {
			t.Fatalf("expected truncated output, got %q (len %s)", result.Stdout, strconv.Itoa(len(result.Stdout)))
		}
	})
}
