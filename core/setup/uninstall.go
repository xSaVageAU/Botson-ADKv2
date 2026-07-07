package setup

import (
	"context"
	"fmt"
	"runtime"

	"botsonv2/core/daemon"
)

// Uninstall stops any running background daemons, removes the tray
// autostart registration and PATH entry, and deletes the installed
// binary. It never touches ~/.botsonv2 (config, sessions, custom agents,
// logs) -- those survive so a future `setup install` picks up where you
// left off.
func Uninstall(ctx context.Context) error {
	confirmed, err := AskYesNo("This will remove Botson from PATH/startup and delete the installed binary (your config and data are kept). Continue?", false)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("Uninstall cancelled.")
		return nil
	}

	stopDaemonQuietly("discord", "Discord gateway")
	stopDaemonQuietly("web", "Web server")
	stopDaemonQuietly("tray", "Tray icon")

	if runtime.GOOS == "windows" {
		if err := UnregisterTrayAutostart(); err != nil {
			fmt.Printf("Warning: failed to remove tray autostart entry: %v\n", err)
		}
	}

	installDir, err := InstallDir()
	if err != nil {
		return err
	}
	if err := RemoveFromPath(installDir); err != nil {
		fmt.Printf("Warning: failed to remove PATH entry automatically: %v\n", err)
	}

	binPath, err := InstalledBinaryPath()
	if err != nil {
		return err
	}
	if err := removeInstalledBinary(binPath); err != nil {
		return fmt.Errorf("failed to remove installed binary: %w", err)
	}

	fmt.Println("Botson has been uninstalled. Your configuration and data remain at ~/.botsonv2.")
	return nil
}

// stopDaemonQuietly stops a background daemon if it's running, falling
// back to a force-kill if graceful shutdown doesn't respond in time.
// Uninstall proceeds regardless -- there's no good reason to abort a
// teardown because a background process was slow to stop.
func stopDaemonQuietly(id, displayName string) {
	if err := daemon.Stop(id, displayName, false); err != nil {
		if err := daemon.Stop(id, displayName, true); err != nil {
			fmt.Printf("Warning: failed to stop %s: %v\n", displayName, err)
		}
	}
}
