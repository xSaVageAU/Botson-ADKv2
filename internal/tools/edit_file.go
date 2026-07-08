package tools

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
)

// EditFileArgs defines the input arguments for the Edit File tool.
type EditFileArgs struct {
	FilePath   string `json:"filePath" jsonschema:"The absolute or relative path (within the project workspace) of the file to edit. The file must already exist and must have been read via readFile earlier in this conversation."`
	OldString  string `json:"oldString" jsonschema:"The exact literal text to find in the file, including whitespace/indentation. Must be unique within the file unless replaceAll is set."`
	NewString  string `json:"newString" jsonschema:"The exact literal text that replaces oldString."`
	ReplaceAll bool   `json:"replaceAll,omitempty" jsonschema:"If true, replaces every occurrence of oldString instead of requiring exactly one match. Defaults to false."`
}

// EditFileResult confirms what was changed.
type EditFileResult struct {
	FilePath            string `json:"filePath"`
	OccurrencesReplaced int    `json:"occurrencesReplaced"`
}

// EditFile allows the agent to make a precise, surgical text replacement in
// an existing file, rather than regenerating (and risking mangling) the
// whole thing via writeFile: oldString must match the file's current
// content exactly, and -- unless replaceAll is set -- must match exactly
// once, so the agent cannot silently touch an unintended occurrence
// elsewhere in the file that happens to share the same text.
func EditFile(ctx agent.Context, args EditFileArgs) (EditFileResult, error) {
	if args.OldString == "" {
		return EditFileResult{}, fmt.Errorf("oldString must not be empty")
	}
	if args.OldString == args.NewString {
		return EditFileResult{}, fmt.Errorf("oldString and newString are identical; nothing to edit")
	}

	fullPath, err := resolveWorkspacePath(args.FilePath)
	if err != nil {
		return EditFileResult{}, err
	}

	if _, err := os.Stat(fullPath); err != nil {
		return EditFileResult{}, fmt.Errorf("cannot edit %s: file does not exist (use writeFile to create a new file)", fullPath)
	}

	if err := requireFileReadBeforeWrite(ctx, fullPath); err != nil {
		return EditFileResult{}, err
	}

	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return EditFileResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(contentBytes)

	count := strings.Count(content, args.OldString)
	switch {
	case count == 0:
		return EditFileResult{}, fmt.Errorf("oldString not found in %s -- it must match the file's existing content exactly, including whitespace", fullPath)
	case count > 1 && !args.ReplaceAll:
		return EditFileResult{}, fmt.Errorf("oldString matches %d locations in %s, expected exactly 1; pass replaceAll to replace all of them, or include more surrounding context in oldString to make it unique", count, fullPath)
	}

	replaced := count
	newContent := strings.ReplaceAll(content, args.OldString, args.NewString)
	if !args.ReplaceAll {
		newContent = strings.Replace(content, args.OldString, args.NewString, 1)
		replaced = 1
	}

	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return EditFileResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// The file's new content is now "known" -- no synthetic re-read needed
	// before a subsequent edit/write in the same turn.
	markFileRead(ctx, fullPath)

	return EditFileResult{FilePath: fullPath, OccurrencesReplaced: replaced}, nil
}
