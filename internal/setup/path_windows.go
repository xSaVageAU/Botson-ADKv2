//go:build windows

package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"botsonv2/internal/config"
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

// writePathBackup saves the pre-change PATH value to a file rather than
// dumping it to the console -- a file survives after the terminal closes
// and doesn't clutter the interactive install flow with a long raw
// semicolon-separated line, while still giving you something to manually
// restore from if a PATH edit ever needs undoing outside of `setup
// uninstall`.
func writePathBackup(current string) error {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("Backup of your user PATH (HKCU\\Environment\\Path) taken by `botson setup install`\n")
	sb.WriteString("before it appended its own install directory.\n\n")
	sb.WriteString("Entries:\n")
	for _, entry := range strings.Split(current, ";") {
		if entry == "" {
			continue
		}
		sb.WriteString("  " + entry + "\n")
	}
	sb.WriteString("\nRaw value (for restoring via `reg add HKCU\\Environment /v Path /t REG_EXPAND_SZ /d \"...\" /f`):\n")
	sb.WriteString(current + "\n")

	return os.WriteFile(filepath.Join(dataDir, "path_backup.txt"), []byte(sb.String()), 0644)
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

	if err := writePathBackup(current); err != nil {
		fmt.Printf("Warning: failed to back up current PATH: %v\n", err)
	} else {
		fmt.Println("Backed up your current PATH to ~/.botsonv2/path_backup.txt before modifying it.")
	}

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

// IsOnPath reports whether dir is currently present in the current user's
// PATH, without modifying anything -- used by `setup status`.
func IsOnPath(dir string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	current, _, err := key.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("failed to read current PATH: %w", err)
	}

	for _, existing := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimSpace(existing), dir) {
			return true, nil
		}
	}
	return false, nil
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
