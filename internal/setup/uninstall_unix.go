//go:build !windows

package setup

import "os"

// removeInstalledBinary deletes the installed binary. Unix lets a process
// unlink its own running executable file directly -- the directory entry
// is removed immediately while the running process keeps its own handle
// to the (now unnamed) inode until it exits, so no deferred-delete trick
// is needed here.
func removeInstalledBinary(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
