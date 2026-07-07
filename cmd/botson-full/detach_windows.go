//go:build windows

package main

import "syscall"

// DETACHED_PROCESS isn't exposed as a named constant in the syscall package,
// so its documented raw value is used directly here.
const detachedProcess = 0x00000008

// detachedSysProcAttr configures the child process to run fully detached
// from the parent's console: no console window, and not part of the
// parent's process group, so closing the parent's terminal (or Ctrl+C to
// it) doesn't affect the child.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}
