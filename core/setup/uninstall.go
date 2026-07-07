package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"botsonv2/core/config"
	"botsonv2/core/daemon"
)

// Uninstall stops any running background daemons, removes the tray
// autostart registration, PATH entry, and installed binary. Unless full is
// true, it then asks whether to keep config.json, deleting everything else
// under ~/.botsonv2 (sessions, custom agents, logs) either way; full skips
// that question and deletes config.json too.
func Uninstall(ctx context.Context, full bool) error {
	confirmed, err := AskYesNo("Are you sure you want to uninstall Botson? This will delete the PATH/Startup and installed binary.", false)
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

	deleteConfig := full
	if !full {
		keepConfig, err := AskYesNo("Do you want to keep your config.json file? Everything else will be deleted.", true)
		if err != nil {
			return err
		}
		deleteConfig = !keepConfig
	}

	if err := wipeDataDir(deleteConfig); err != nil {
		return err
	}

	if deleteConfig {
		fmt.Println("Botson has been uninstalled and all data removed.")
	} else {
		fmt.Println("Botson has been uninstalled. Your config.json remains at ~/.botsonv2.")
	}
	return nil
}

// wipeDataDir deletes everything under the data directory except "bin"
// (its binary removal is handled separately above, and may still be
// pending via a deferred delete on Windows) and, unless deleteConfig is
// true, "config.json".
func wipeDataDir(deleteConfig bool) error {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "bin" {
			continue
		}
		if name == "config.json" && !deleteConfig {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dataDir, name)); err != nil {
			return fmt.Errorf("failed to remove %s: %w", name, err)
		}
	}

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
