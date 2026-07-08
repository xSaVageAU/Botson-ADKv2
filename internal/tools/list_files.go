package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/adk/v2/agent"
)

// InspectWorkspaceArgs defines the input arguments for listing files in the workspace.
type InspectWorkspaceArgs struct {
	Subdir string `json:"subDir" jsonschema:"Optional relative subdirectory within the workspace to list. Defaults to root."`
}

// InspectWorkspaceResult defines the list of files found.
type InspectWorkspaceResult struct {
	CurrentPath string   `json:"currentPath"`
	Files       []string `json:"files"`
}

// ListFiles allows the agent to check the local project files to help the developer.
func ListFiles(ctx agent.Context, args InspectWorkspaceArgs) (InspectWorkspaceResult, error) {
	root, err := os.Getwd()
	if err != nil {
		return InspectWorkspaceResult{}, fmt.Errorf("failed to get current working directory: %w", err)
	}

	targetDir := root
	if args.Subdir != "" {
		cleanedSub := filepath.Clean(args.Subdir)
		if strings.HasPrefix(cleanedSub, "..") || filepath.IsAbs(cleanedSub) {
			return InspectWorkspaceResult{}, fmt.Errorf("access denied: path must be inside project workspace")
		}
		targetDir = filepath.Join(root, cleanedSub)
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return InspectWorkspaceResult{}, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		files = append(files, name)
	}

	relPath := "."
	if args.Subdir != "" {
		relPath = args.Subdir
	}

	return InspectWorkspaceResult{
		CurrentPath: relPath,
		Files:       files,
	}, nil
}
