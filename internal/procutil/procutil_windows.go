//go:build windows

package procutil

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setNewProcessGroup puts cmd in its own process group and, on context
// cancellation, kills the whole tree via taskkill /T rather than just the
// single pid Go tracks. See the package doc for why this matters.
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
