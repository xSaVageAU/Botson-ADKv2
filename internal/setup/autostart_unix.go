//go:build !windows

package setup

import "fmt"

// RegisterTrayAutostart is not offered on Linux -- the tray itself is
// currently Windows-only.
func RegisterTrayAutostart(exePath string) error {
	return fmt.Errorf("tray autostart is not supported on this platform")
}

// UnregisterTrayAutostart is a no-op on Linux since autostart is never
// registered here.
func UnregisterTrayAutostart() error {
	return nil
}

// IsTrayAutostartRegistered always reports false on Linux since autostart
// is never registered here -- used by `setup status`.
func IsTrayAutostartRegistered() (bool, error) {
	return false, nil
}
