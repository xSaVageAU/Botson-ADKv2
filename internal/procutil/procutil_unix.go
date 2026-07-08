//go:build !windows

package procutil

import (
	"os/exec"
	"syscall"
)

// setNewProcessGroup puts cmd in its own process group and, on context
// cancellation, kills that whole group rather than just the single pid Go
// tracks. See the package doc for why this matters.
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
