package scripts

import (
	"context"
	"strings"
	"testing"
)

const helloSource = `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("hello from script")
	if len(os.Args) > 1 {
		fmt.Println("arg:", os.Args[1])
	}
}
`

func TestSaveListShowDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := Save(Detail{Name: "hello", Description: "prints a greeting", Source: helloSource}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	details, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 script, got %d", len(details))
	}
	if details[0].Name != "hello" || details[0].Description != "prints a greeting" {
		t.Fatalf("unexpected detail: %+v", details[0])
	}
	if !strings.Contains(details[0].Source, "hello from script") {
		t.Fatalf("expected source to round-trip, got %q", details[0].Source)
	}

	if err := Delete("hello"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	details, err = List()
	if err != nil {
		t.Fatalf("List after delete failed: %v", err)
	}
	if len(details) != 0 {
		t.Fatalf("expected 0 scripts after delete, got %d", len(details))
	}

	if err := Delete("hello"); !isNotFound(err) {
		t.Fatalf("expected ErrScriptNotFound deleting an already-deleted script, got %v", err)
	}
}

func TestSaveRejectsInvalidName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := Save(Detail{Name: "not a valid name!", Source: helloSource}); err == nil {
		t.Fatal("expected an error for an invalid script name, got none")
	}
}

func TestRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	if err := Save(Detail{Name: "hello", Source: helloSource}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	result, err := Run(context.Background(), "hello", []string{"world"}, 0)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello from script") {
		t.Fatalf("expected stdout to contain the script's output, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "arg: world") {
		t.Fatalf("expected stdout to contain the passed arg, got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
}

// TestRunPreservesExitCode guards against a real bug found while building
// this: running a script via `go run` always reports exit code 1 on any
// non-zero child exit, discarding the script's actual code. Run must
// build the script to a temp binary and execute that directly instead.
func TestRunPreservesExitCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	const exitCodeSource = `package main

import "os"

func main() {
	os.Exit(7)
}
`
	if err := Save(Detail{Name: "exiter", Source: exitCodeSource}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	result, err := Run(context.Background(), "exiter", nil, 0)
	if err != nil {
		t.Fatalf("Run should report a non-zero exit via ExitCode, not err: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected the script's real exit code 7 to be preserved, got %d", result.ExitCode)
	}
}

func TestRunRejectsUnknownScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := Run(context.Background(), "nonexistent", nil, 0); !isNotFound(err) {
		t.Fatalf("expected ErrScriptNotFound for an unknown script, got %v", err)
	}
}

func isNotFound(err error) bool {
	return err == ErrScriptNotFound
}
