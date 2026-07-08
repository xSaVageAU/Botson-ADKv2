//go:build windows

package tools

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setNewProcessGroup puts cmd in its own process group and, on context
// cancellation, kills the whole tree via taskkill /T rather than just the
// single pid Go tracks -- same reasoning as the Unix version: a command
// that spawns its own children shouldn't be able to outlive the timeout
// that was meant to stop it.
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
