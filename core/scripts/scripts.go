// Package scripts implements Botson's named-script system: small Go
// programs, written by the user or the agent itself, saved under
// ~/.botsonv2/scripts/<name>/main.go and runnable by name (built, then
// executed directly -- see Run) rather than a fixed one-off CLI
// subcommand. It's a leaf package (like
// core/config) with no dependency on core/agent or core/management, so
// both the CLI (cmd/botson) and the runScript/saveScript agent tools
// (core/tools) can depend on it directly without an import cycle.
package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"botsonv2/core/config"
	"botsonv2/core/procutil"
)

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Sentinel errors so callers (CLI, agent tool) can map failures without
// string-matching error text -- same pattern as core/management's agent
// errors.
var (
	ErrInvalidScriptName = errors.New("invalid script name: must contain only alphanumeric characters, underscores, and dashes")
	ErrScriptNotFound    = errors.New("script not found")
)

// Detail describes one saved script.
type Detail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// manifest is the small JSON sidecar (script.json) holding metadata that
// doesn't belong inside the Go source itself.
type manifest struct {
	Description string `json:"description"`
}

// GetDataDir resolves the physical path to ~/.botsonv2/scripts/ and
// ensures it exists.
func GetDataDir() (string, error) {
	baseDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	dataDir := filepath.Join(baseDir, "scripts")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create scripts directory: %w", err)
	}
	return dataDir, nil
}

// List returns every saved script (name, description, and full source),
// sorted by name.
func List() ([]Detail, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read scripts directory: %w", err)
	}

	var details []Detail
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		src, err := os.ReadFile(filepath.Join(dataDir, name, "main.go"))
		if err != nil {
			continue // not a valid script directory (missing main.go); skip
		}

		var m manifest
		if data, err := os.ReadFile(filepath.Join(dataDir, name, "script.json")); err == nil {
			_ = json.Unmarshal(data, &m)
		}

		details = append(details, Detail{
			Name:        name,
			Description: m.Description,
			Source:      string(src),
		})
	}

	sort.Slice(details, func(i, j int) bool { return details[i].Name < details[j].Name })
	return details, nil
}

// Save validates and persists a script's main.go and script.json to
// ~/.botsonv2/scripts/<name>/, overwriting it if the name already exists.
func Save(detail Detail) error {
	detail.Name = strings.TrimSpace(detail.Name)
	if detail.Name == "" || !nameRegex.MatchString(detail.Name) {
		return ErrInvalidScriptName
	}

	dataDir, err := GetDataDir()
	if err != nil {
		return err
	}

	scriptDir := filepath.Join(dataDir, detail.Name)
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return fmt.Errorf("failed to create script directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(scriptDir, "main.go"), []byte(detail.Source), 0644); err != nil {
		return fmt.Errorf("failed to write main.go: %w", err)
	}

	manifestBytes, err := json.MarshalIndent(manifest{Description: detail.Description}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize script manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "script.json"), manifestBytes, 0644); err != nil {
		return fmt.Errorf("failed to write script.json: %w", err)
	}

	return nil
}

// Delete removes a saved script's directory entirely.
func Delete(name string) error {
	if name == "" || !nameRegex.MatchString(name) {
		return ErrInvalidScriptName
	}

	dataDir, err := GetDataDir()
	if err != nil {
		return err
	}

	scriptDir := filepath.Join(dataDir, name)
	if _, err := os.Stat(scriptDir); os.IsNotExist(err) {
		return ErrScriptNotFound
	}

	if err := os.RemoveAll(scriptDir); err != nil {
		return fmt.Errorf("failed to delete script directory: %w", err)
	}

	return nil
}

// Run executes a saved script by name via `go run`, in the caller's
// current working directory (so a script can act on the actual project,
// not just its own definition folder), with a timeout. timeoutSeconds <=
// 0 uses procutil.DefaultTimeout.
func Run(ctx context.Context, name string, args []string, timeoutSeconds int) (procutil.Result, error) {
	if name == "" || !nameRegex.MatchString(name) {
		return procutil.Result{}, ErrInvalidScriptName
	}

	dataDir, err := GetDataDir()
	if err != nil {
		return procutil.Result{}, err
	}

	mainPath := filepath.Join(dataDir, name, "main.go")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		return procutil.Result{}, ErrScriptNotFound
	}

	root, err := os.Getwd()
	if err != nil {
		return procutil.Result{}, fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Built to a temp binary and executed directly rather than `go run
	// <path>`: go run always exits 1 on a non-zero child exit regardless
	// of the actual code (it just prints "exit status N" to stderr),
	// which would silently lose the real exit code -- the whole point of
	// reporting one back to the caller. Building still benefits from
	// Go's own build cache, so repeat runs stay fast.
	tmpDir, err := os.MkdirTemp("", "botson-script-*")
	if err != nil {
		return procutil.Result{}, fmt.Errorf("failed to create temp build directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "script")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	// The build step always gets a flat, generous timeout of its own --
	// timeoutSeconds describes how long the script's own execution may
	// take, not how long compiling it takes.
	buildResult, err := procutil.Run(ctx, "go", []string{"build", "-o", binPath, mainPath}, procutil.RunOptions{Dir: root})
	if err != nil {
		return procutil.Result{}, fmt.Errorf("failed to build script: %w", err)
	}
	if buildResult.ExitCode != 0 {
		return procutil.Result{}, fmt.Errorf("script failed to compile:\n%s", buildResult.Stderr)
	}

	var timeout time.Duration
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	return procutil.Run(ctx, binPath, args, procutil.RunOptions{
		Dir:     root,
		Timeout: timeout,
	})
}
