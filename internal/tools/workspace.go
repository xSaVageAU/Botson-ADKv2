package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveWorkspacePath validates a user-supplied (relative or absolute) path
// against the current workspace root, returning the resolved absolute path.
// It blocks two things: escaping the workspace root entirely, and touching
// the loaded .env file. Shared by every tool that reads or writes real
// files (readFile, writeFile) so the one security-sensitive check lives in
// exactly one place.
func resolveWorkspacePath(relOrAbs string) (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	cleaned := filepath.Clean(relOrAbs)
	full := cleaned
	if !filepath.IsAbs(cleaned) {
		full = filepath.Join(root, cleaned)
	}

	// A plain strings.HasPrefix(full, root) would let a sibling directory
	// that merely shares root's string prefix (e.g. root "/proj" matching
	// "/proj-evil") slip through; requiring a path-separator boundary (or
	// an exact match) closes that gap.
	rootWithSep := root
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	if full != root && !strings.HasPrefix(full, rootWithSep) {
		return "", fmt.Errorf("access denied: path must be inside project workspace")
	}

	envPath := filepath.Clean(filepath.Join(root, ".env"))
	if strings.EqualFold(filepath.Clean(full), envPath) {
		return "", fmt.Errorf("access denied: cannot access the configuration environment file")
	}

	return full, nil
}
