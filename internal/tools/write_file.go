package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/adk/v2/agent"
)

// WriteFileArgs defines the input arguments for the Write File tool.
type WriteFileArgs struct {
	FilePath string `json:"filePath" jsonschema:"The absolute or relative path (within the project workspace) to write to. Parent directories are created automatically."`
	Content  string `json:"content" jsonschema:"The full text content to write. This replaces the entire file if it already exists."`
}

// WriteFileResult confirms what was written.
type WriteFileResult struct {
	FilePath     string `json:"filePath"`
	BytesWritten int    `json:"bytesWritten"`
}

// WriteFile allows the agent to create or overwrite a file in the project
// workspace -- the counterpart to ReadFile/ListFiles that actually lets it
// make changes, rather than only saving to the separate, session-scoped
// artifact store SaveArtifact writes to.
func WriteFile(ctx agent.Context, args WriteFileArgs) (WriteFileResult, error) {
	fullPath, err := resolveWorkspacePath(args.FilePath)
	if err != nil {
		return WriteFileResult{}, err
	}

	if err := requireFileReadBeforeWrite(ctx, fullPath); err != nil {
		return WriteFileResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return WriteFileResult{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(args.Content), 0644); err != nil {
		return WriteFileResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	markFileRead(ctx, fullPath)

	return WriteFileResult{
		FilePath:     fullPath,
		BytesWritten: len(args.Content),
	}, nil
}
