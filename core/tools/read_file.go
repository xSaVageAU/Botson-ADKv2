package tools

import (
	"fmt"
	"os"

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
	fullPath, err := resolveWorkspacePath(args.FilePath)
	if err != nil {
		return ReadFileResult{}, err
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	return ReadFileResult{
		Content: string(content),
	}, nil
}
