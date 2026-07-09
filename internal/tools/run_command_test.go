package tools

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestRunCommand(t *testing.T) {
	root := t.TempDir()

	t.Run("captures stdout and exit code 0", func(t *testing.T) {
		result, err := runCommand(context.Background(), root, RunCommandArgs{Command: echoCommand("hello")})
		if err != nil {
			t.Fatalf("runCommand failed: %v", err)
		}
		if !strings.Contains(result.Stdout, "hello") {
			t.Fatalf("expected stdout to contain %q, got %q", "hello", result.Stdout)
		}
		if result.ExitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", result.ExitCode)
		}
	})

	t.Run("captures non-zero exit code without erroring", func(t *testing.T) {
		result, err := runCommand(context.Background(), root, RunCommandArgs{Command: exitCommand(3)})
		if err != nil {
			t.Fatalf("runCommand should report a non-zero exit via ExitCode, not err: %v", err)
		}
		if result.ExitCode != 3 {
			t.Fatalf("expected exit code 3, got %d", result.ExitCode)
		}
	})

	t.Run("rejects an empty command", func(t *testing.T) {
		if _, err := runCommand(context.Background(), root, RunCommandArgs{Command: "   "}); err == nil {
			t.Fatal("expected an error for an empty command, got none")
		}
	})

	t.Run("times out a long-running command", func(t *testing.T) {
		_, err := runCommand(context.Background(), root, RunCommandArgs{
			Command:        sleepCommand(5),
			TimeoutSeconds: 1,
		})
		if err == nil {
			t.Fatal("expected a timeout error, got none")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected a timeout error, got: %v", err)
		}
	})
}

func echoCommand(s string) string {
	return "echo " + s
}

func exitCommand(code int) string {
	return "exit " + strconv.Itoa(code)
}

func sleepCommand(seconds int) string {
	if runtime.GOOS == "windows" {
		return "timeout /T " + strconv.Itoa(seconds)
	}
	return "sleep " + strconv.Itoa(seconds)
}
