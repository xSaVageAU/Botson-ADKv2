package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/adk/v2/agent"
)

// WorkspaceRoot is the default directory the workspace-touching tools
// operate in, set once at boot (see SetWorkspaceRoot).
var WorkspaceRoot string

// SetWorkspaceRoot sets WorkspaceRoot. Called once at boot from the
// configured AppConfig.WorkspaceRoot, and again any time it's changed live
// via botson.settings.set (internal/natsapi/server.go).
func SetWorkspaceRoot(root string) {
	WorkspaceRoot = root
}

// cwdStateKey is the reserved session-state key a consumer sets via
// stateDelta on /api/run (adk.rest.POST /api/run) to override the working
// directory for a session -- see docs/nats-api.md. Unlike WorkspaceRoot,
// an override is not sandboxed to any particular root: it may be any
// absolute path the core process can read/write. That trade (flexibility
// over a path jail) is why the embedded NATS server requires an auth
// token (AppConfig.NatsAuthToken) -- see cmd/botson-core/cmd_core.go.
const cwdStateKey = "botson:cwd"

// effectiveRoot resolves the root a tool call should operate in: the
// session's "botson:cwd" state override if it has one set, else the
// configured WorkspaceRoot. Permissive of a nil ctx/State, matching the
// same convention read_tracking.go's markFileRead/wasFileRead already
// use -- production tool invocation always supplies a real context; this
// only matters for keeping simple tests simple.
func effectiveRoot(ctx agent.Context) string {
	if ctx == nil {
		return WorkspaceRoot
	}
	state := ctx.State()
	if state == nil {
		return WorkspaceRoot
	}
	if v, err := state.Get(cwdStateKey); err == nil {
		if cwd, ok := v.(string); ok && cwd != "" {
			return cwd
		}
	}
	return WorkspaceRoot
}

// resolveWorkspacePath validates a user-supplied (relative or absolute) path
// against the effective root for this call (see effectiveRoot), returning
// the resolved absolute path. It blocks two things: escaping that root
// entirely, and touching the loaded .env file. Shared by every tool that
// reads or writes real files (readFile, writeFile, editFile, listFiles) so
// the one security-sensitive check lives in exactly one place.
func resolveWorkspacePath(ctx agent.Context, relOrAbs string) (string, error) {
	root := effectiveRoot(ctx)

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
