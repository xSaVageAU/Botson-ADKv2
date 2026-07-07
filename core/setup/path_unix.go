//go:build !windows

package setup

import (
	"fmt"
	"os"
	"strings"
)

// AddToPath cannot safely edit an arbitrary shell's rc file automatically
// (there's no single canonical place like Windows' per-user registry key),
// so instead it checks whether dir is already on $PATH and, if not, prints
// a copy-pastable instruction for the user to add it themselves.
func AddToPath(dir string) error {
	for _, p := range strings.Split(os.Getenv("PATH"), ":") {
		if p == dir {
			return nil
		}
	}
	fmt.Printf("\n%s is not on your PATH yet. Add this to your shell profile (~/.profile, ~/.bashrc, etc.):\n\n  export PATH=\"$PATH:%s\"\n\n", dir, dir)
	return nil
}

// RemoveFromPath is a no-op on Linux since AddToPath never edits a shell
// profile automatically -- there's nothing this process added to undo.
func RemoveFromPath(dir string) error {
	return nil
}

// IsOnPath reports whether dir is currently present in $PATH -- used by
// `setup status`.
func IsOnPath(dir string) (bool, error) {
	for _, p := range strings.Split(os.Getenv("PATH"), ":") {
		if p == dir {
			return true, nil
		}
	}
	return false, nil
}
