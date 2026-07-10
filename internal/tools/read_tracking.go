package tools

import (
	"fmt"
	"os"

	"google.golang.org/adk/v2/agent"
)

// readTrackingKeyPrefix namespaces the per-file session-state keys this
// package uses to remember which workspace files have been read via
// ReadFile during the current session. One key per absolute path (rather
// than a single map-valued key) sidesteps the fact that session state is
// JSON round-tripped on reload -- a bool survives that round trip
// losslessly as a bool, whereas a map[string]bool would come back as
// map[string]interface{}.
const readTrackingKeyPrefix = "botson:tools:read:"

func readTrackingKey(fullPath string) string {
	return readTrackingKeyPrefix + fullPath
}

// markFileRead records, in the session's durable state, that fullPath (an
// absolute path already resolved by resolveWorkspacePath) has been read via
// ReadFile in this session. Deliberately a no-op if ctx or ctx.State() is
// unavailable (e.g. nil, as every tool-function unit test that doesn't
// specifically exercise this guard does today) -- production tool
// invocation always supplies a real context, so this path only matters for
// keeping simple tests simple.
func markFileRead(ctx agent.Context, fullPath string) {
	if ctx == nil {
		return
	}
	state := ctx.State()
	if state == nil {
		return
	}
	_ = state.Set(readTrackingKey(fullPath), true)
}

// wasFileRead reports whether fullPath was previously marked read via
// markFileRead earlier in this session. Permissive (returns true) if
// ctx/State is unavailable, so the read-before-write guard only actively
// engages when tracking is actually possible.
func wasFileRead(ctx agent.Context, fullPath string) bool {
	if ctx == nil {
		return true
	}
	state := ctx.State()
	if state == nil {
		return true
	}
	val, err := state.Get(readTrackingKey(fullPath))
	if err != nil {
		return false
	}
	read, _ := val.(bool)
	return read
}

// requireFileReadBeforeWrite is the shared "must read before you write"
// guard used by both WriteFile and EditFile. A file that does not yet
// exist is exempt (there's nothing to have read); an existing file must
// have been read via ReadFile earlier in this session.
func requireFileReadBeforeWrite(ctx agent.Context, fullPath string) error {
	if _, err := os.Stat(fullPath); err != nil {
		return nil // new (or inaccessible) file: nothing to enforce
	}
	if !wasFileRead(ctx, fullPath) {
		return fmt.Errorf("%s already exists on disk with content you haven't seen yet in this conversation -- you must read it with readFile at least once before writing or editing it, so you don't blindly overwrite content you're not aware of", fullPath)
	}
	return nil
}
