package setup

import (
	"path/filepath"
	"runtime"

	"botsonv2/internal/config"
)

// binaryName is the stable, platform-appropriate name the binary is
// installed under, regardless of what the original downloaded file was
// called (e.g. botsonv2-windows-amd64.exe).
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "botson.exe"
	}
	return "botson"
}

// InstallDir returns the directory the binary is installed into --
// <data dir>/bin, keeping everything Botson owns under the one
// ~/.botsonv2 folder rather than inventing a second location convention.
func InstallDir() (string, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "bin"), nil
}

// InstalledBinaryPath returns the full path the binary is (or would be)
// installed at.
func InstalledBinaryPath() (string, error) {
	dir, err := InstallDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, binaryName()), nil
}
