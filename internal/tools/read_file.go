package tools

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
)

const defaultReadLineLimit = 2000

// ReadFileArgs defines the input arguments for the Read File tool.
type ReadFileArgs struct {
	FilePath string `json:"filePath" jsonschema:"The absolute or relative path to the file to read."`
	Offset   int    `json:"offset,omitempty" jsonschema:"1-based line number to start reading from. Defaults to 1 (the beginning of the file)."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum number of lines to return in this call. Defaults to 2000. Use offset on a follow-up call to read further into a large file."`
}

// ReadFileResult defines the paginated, line-numbered content of the file.
type ReadFileResult struct {
	// Content is formatted like `cat -n`: each line is prefixed with its
	// 1-based line number, e.g. "     1\tpackage main\n", so a line number
	// shown here can be quoted back precisely in a subsequent editFile call.
	Content    string `json:"content"`
	TotalLines int    `json:"totalLines"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	Truncated  bool   `json:"truncated"`
}

// ReadFile allows the agent to read the content of a specific file, in
// line-numbered, paginated chunks. A successful read of any range marks
// the whole file as "read" for the purposes of WriteFile/EditFile's
// read-before-write guard -- a single ReadFile call anywhere in the file
// is enough to unlock editing it, not a full-file read.
func ReadFile(ctx agent.Context, args ReadFileArgs) (ReadFileResult, error) {
	fullPath, err := resolveWorkspacePath(ctx, args.FilePath)
	if err != nil {
		return ReadFileResult{}, err
	}

	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(contentBytes), "\n")
	// A trailing newline in the file produces one bogus empty trailing
	// element from strings.Split; drop it so TotalLines matches what an
	// editor or `wc -l` would report.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	totalLines := len(lines)

	offset := args.Offset
	if offset < 1 {
		offset = 1
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultReadLineLimit
	}

	startIdx := offset - 1
	if startIdx > totalLines {
		startIdx = totalLines
	}
	endIdx := startIdx + limit
	if endIdx > totalLines {
		endIdx = totalLines
	}

	var b strings.Builder
	for i := startIdx; i < endIdx; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}

	markFileRead(ctx, fullPath)

	return ReadFileResult{
		Content:    b.String(),
		TotalLines: totalLines,
		StartLine:  startIdx + 1,
		EndLine:    endIdx,
		Truncated:  endIdx < totalLines,
	}, nil
}
