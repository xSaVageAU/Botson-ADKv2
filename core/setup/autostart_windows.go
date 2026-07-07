//go:build windows

package setup

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const autostartValueName = "Botson"
const autostartKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// RegisterTrayAutostart registers the tray icon to launch at login via a
// per-user Run key -- a plain string value, no shortcut (.lnk) file needed.
func RegisterTrayAutostart(exePath string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	command := fmt.Sprintf(`"%s" tray start`, exePath)
	if err := key.SetStringValue(autostartValueName, command); err != nil {
		return fmt.Errorf("failed to register autostart: %w", err)
	}
	return nil
}

// UnregisterTrayAutostart removes the Run key entry, if present.
func UnregisterTrayAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(autostartValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("failed to remove autostart entry: %w", err)
	}
	return nil
}
