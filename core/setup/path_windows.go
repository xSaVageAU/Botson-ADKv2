//go:build windows

package setup

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	smtoAbortIfHung = 0x0002
	wmSettingChange = 0x001A
	hwndBroadcast   = 0xffff
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// broadcastEnvironmentChange notifies running processes (Explorer, open
// shells) that the environment changed, so PATH updates are picked up
// without requiring a logoff. This is the standard technique every
// Windows installer uses; there's no ready-made wrapper for it in
// golang.org/x/sys/windows, hence the manual syscall.
func broadcastEnvironmentChange() {
	param, _ := syscall.UTF16PtrFromString("Environment")
	procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(param)),
		uintptr(smtoAbortIfHung),
		5000,
		0,
	)
}

// AddToPath appends dir to the current user's PATH (HKCU\Environment), if
// it isn't already present.
func AddToPath(dir string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	current, _, err := key.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("failed to read current PATH: %w", err)
	}

	for _, existing := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimSpace(existing), dir) {
			return nil // already present
		}
	}

	fmt.Printf("Current user PATH (before change): %s\n", current)

	updated := dir
	if current != "" {
		updated = current + ";" + dir
	}

	if err := key.SetExpandStringValue("Path", updated); err != nil {
		return fmt.Errorf("failed to update PATH: %w", err)
	}

	broadcastEnvironmentChange()
	return nil
}

// RemoveFromPath removes dir from the current user's PATH, if present.
func RemoveFromPath(dir string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	current, _, err := key.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("failed to read current PATH: %w", err)
	}

	var kept []string
	changed := false
	for _, existing := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimSpace(existing), dir) {
			changed = true
			continue
		}
		if existing != "" {
			kept = append(kept, existing)
		}
	}
	if !changed {
		return nil
	}

	if err := key.SetExpandStringValue("Path", strings.Join(kept, ";")); err != nil {
		return fmt.Errorf("failed to update PATH: %w", err)
	}

	broadcastEnvironmentChange()
	return nil
}
