package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/adk/v2/agent"
)

// ReadFileArgs defines the input arguments for the Read File tool.
type ReadFileArgs struct {
	FilePath string `json:"filePath" jsonschema:"The absolute or relative path to the file to read."`
}

// ReadFileResult defines the output content of the file.
type ReadFileResult struct {
	Content string `json:"content"`
}

// ReadFile allows the agent to read the content of a specific file.
func ReadFile(ctx agent.Context, args ReadFileArgs) (ReadFileResult, error) {
	root, err := os.Getwd()
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Clean and secure path construction to avoid directory traversal
	cleanedPath := filepath.Clean(args.FilePath)
	
	// Ensure path is relative or absolute inside the project
	fullPath := cleanedPath
	if !filepath.IsAbs(cleanedPath) {
		fullPath = filepath.Join(root, cleanedPath)
	}

	if !strings.HasPrefix(fullPath, root) {
		return ReadFileResult{}, fmt.Errorf("access denied: file path must be inside project workspace")
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	return ReadFileResult{
		Content: string(content),
	}, nil
}
