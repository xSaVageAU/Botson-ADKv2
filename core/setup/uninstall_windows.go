//go:build windows

package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const detachedProcessFlag = 0x00000008

// scheduleSelfDelete spawns a short detached helper that waits for this
// process to exit, then deletes path. Windows won't let a running process
// delete (or be overwritten at) its own executable file, so the delete has
// to happen from a separate process after this one exits.
//
// The helper is written out as a temp .bat file rather than passed as an
// inline `cmd /C "..."` string -- nesting the quoted target path inside
// Go's own argument-escaping for an inline string runs straight into
// cmd.exe's well-known quoting ambiguities (silently produced a no-op del
// during testing). A batch file's own contents need no such escaping.
// It retries the delete in a loop (rather than a single fixed delay)
// since there's no reliable fixed time by which this process is
// guaranteed to have released its file handle, then deletes itself.
func scheduleSelfDelete(path string) error {
	batPath := filepath.Join(os.TempDir(), fmt.Sprintf("botson-uninstall-%d.bat", os.Getpid()))
	script := "@echo off\r\n" +
		":retry\r\n" +
		"del /F /Q \"" + path + "\" >NUL 2>&1\r\n" +
		"if exist \"" + path + "\" (\r\n" +
		"  ping -n 2 127.0.0.1 >NUL\r\n" +
		"  goto retry\r\n" +
		")\r\n" +
		"del /F /Q \"%~f0\"\r\n"

	if err := os.WriteFile(batPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("failed to write uninstall helper script: %w", err)
	}

	cmd := exec.Command("cmd", "/C", batPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcessFlag,
	}
	return cmd.Start()
}

// removeInstalledBinary deletes the installed binary. On Windows this is
// always the currently-running executable (uninstall re-execs the
// installed copy), so the delete must be deferred until after this
// process exits.
func removeInstalledBinary(path string) error {
	return scheduleSelfDelete(path)
}
