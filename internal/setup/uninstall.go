package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"botsonv2/internal/config"
	"botsonv2/internal/daemon"
)

// Uninstall asks, step by step, which parts of the installation to remove
// -- the PATH entry, the tray autostart registration (Windows), and the
// installed binary -- so any one of them can be done alone (e.g. "just
// take it off PATH") instead of as one all-or-nothing operation. Deleting
// the binary is treated as the actual "uninstall": only then does it ask
// whether to keep config.json, and wipe everything else under
// ~/.botsonv2 either way. If forceFull is true, none of these questions
// are asked at all -- every step runs and config.json is deleted too, for
// a completely unattended full wipe.
func Uninstall(ctx context.Context, forceFull bool) error {
	fmt.Println("Botson Setup - Uninstall")
	fmt.Println("=========================")

	if forceFull {
		fmt.Println("--force-full-uninstall: skipping all prompts, removing everything.")
	}

	removePath, err := confirmStep(forceFull, "Remove Botson from PATH?")
	if err != nil {
		return err
	}

	removeStartup := false
	if runtime.GOOS == "windows" {
		removeStartup, err = confirmStep(forceFull, "Remove Botson from Startup (tray autostart at login)?")
		if err != nil {
			return err
		}
	}

	removeBinary, err := confirmStep(forceFull, "Delete the installed binary?")
	if err != nil {
		return err
	}

	if !removePath && !removeStartup && !removeBinary {
		fmt.Println("Nothing selected; uninstall cancelled.")
		return nil
	}

	if removeBinary {
		// Daemons hold their own handle on the same installed binary, so
		// they need to stop before it can be deleted; not needed for the
		// lighter PATH/Startup-only paths.
		stopDaemonQuietly("discord", "Discord gateway")
		stopDaemonQuietly("web", "Web server")
		stopDaemonQuietly("tray", "Tray icon")
	}

	if removeStartup {
		if err := UnregisterTrayAutostart(); err != nil {
			fmt.Printf("Warning: failed to remove tray autostart entry: %v\n", err)
		} else {
			fmt.Println("Removed from Startup.")
		}
	}

	if removePath {
		installDir, err := InstallDir()
		if err != nil {
			return err
		}
		if err := RemoveFromPath(installDir); err != nil {
			fmt.Printf("Warning: failed to remove PATH entry automatically: %v\n", err)
		} else {
			fmt.Println("Removed from PATH.")
		}
	}

	if !removeBinary {
		fmt.Println("Done.")
		return nil
	}

	deleteConfig := forceFull
	if !forceFull {
		keepConfig, err := AskYesNo("Do you want to keep your config.json file? Everything else will be deleted.", true)
		if err != nil {
			return err
		}
		deleteConfig = !keepConfig
	}

	if err := wipeDataDir(deleteConfig); err != nil {
		return err
	}

	// Scheduled last, right before returning: on Windows this process is
	// usually the installed binary itself, so the file can't actually be
	// deleted until this process exits -- the background helper retries
	// in a loop until then. Scheduling it here (rather than earlier)
	// keeps that retry window to roughly "until this process exits",
	// instead of "for as long as the user takes to answer prompts after
	// this point".
	binPath, err := InstalledBinaryPath()
	if err != nil {
		return err
	}
	if err := removeInstalledBinary(binPath); err != nil {
		return fmt.Errorf("failed to remove installed binary: %w", err)
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

// confirmStep asks label (defaulting to yes) unless forceFull is set, in
// which case it returns true immediately without prompting -- shared by
// every yes/no step in Uninstall so --force-full-uninstall skips all of
// them uniformly.
func confirmStep(forceFull bool, label string) (bool, error) {
	if forceFull {
		return true, nil
	}
	return AskYesNo(label, true)
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
