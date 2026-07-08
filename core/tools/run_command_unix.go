//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setNewProcessGroup puts cmd in its own process group and, on context
// cancellation, kills that whole group rather than just the single pid Go
// tracks. A shell command that forks a child (e.g. `sh -c "sleep 5"`,
// which doesn't always exec-replace itself with the child) would
// otherwise leave that child running past the shell's own death --
// keeping the piped stdout/stderr open, so cmd.Run() blocks until the
// orphan exits on its own instead of at the intended deadline.
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
