//go:build !windows

package main

import "syscall"

// detachedSysProcAttr starts the child in its own session, detaching it
// from the parent's controlling terminal.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
