package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"botsonv2/core/config"
	"botsonv2/core/daemon"
	"botsonv2/core/interface/apiclient"
)

// Status reports whether this machine is set up the way `setup install`
// leaves it -- config populated, binary installed, PATH updated, tray
// autostart registered, and which background services are currently
// running -- without modifying anything. Useful for sanity-checking an
// install without digging through the registry or ~/.botsonv2 by hand.
func Status(ctx context.Context) error {
	fmt.Println("Botson Setup - Status")
	fmt.Println("=====================")

	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}
	fmt.Printf("Data directory:   %s\n", dataDir)

	printConfigStatus()
	printBinaryStatus()
	printPathStatus()
	printAutostartStatus()
	printDaemonStatus(ctx)

	return nil
}

func printConfigStatus() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Config:           error loading config.json: %v\n", err)
		return
	}
	fmt.Printf("Gemini API key:   %s\n", presence(cfg.GeminiAPIKey != ""))
	fmt.Printf("Root agent:       %s\n", nonEmpty(cfg.RootAgent, "(not set)"))
	fmt.Printf("Discord:          %s\n", presence(cfg.Discord.Token != ""))
}

func printBinaryStatus() {
	binPath, err := InstalledBinaryPath()
	if err != nil {
		fmt.Printf("Installed binary: error: %v\n", err)
		return
	}

	info, err := os.Stat(binPath)
	if err != nil {
		fmt.Printf("Installed binary: not found at %s\n", binPath)
		return
	}

	suffix := ""
	if exePath, err := os.Executable(); err == nil {
		exeAbs, _ := filepath.EvalSymlinks(exePath)
		binAbs, _ := filepath.EvalSymlinks(binPath)
		if exeAbs != "" && exeAbs == binAbs {
			suffix = " (currently running from here)"
		}
	}
	fmt.Printf("Installed binary: %s, %s%s\n", binPath, formatSize(info.Size()), suffix)
}

func printPathStatus() {
	installDir, err := InstallDir()
	if err != nil {
		fmt.Printf("PATH:             error: %v\n", err)
		return
	}
	onPath, err := IsOnPath(installDir)
	if err != nil {
		fmt.Printf("PATH:             error checking: %v\n", err)
		return
	}
	if onPath {
		fmt.Printf("PATH:             %s is on PATH\n", installDir)
	} else {
		fmt.Printf("PATH:             %s is NOT on PATH\n", installDir)
	}
}

func printAutostartStatus() {
	if runtime.GOOS != "windows" {
		fmt.Println("Tray autostart:   not supported on this platform")
		return
	}
	registered, err := IsTrayAutostartRegistered()
	if err != nil {
		fmt.Printf("Tray autostart:   error checking: %v\n", err)
		return
	}
	if registered {
		fmt.Println("Tray autostart:   enabled (starts at login)")
	} else {
		fmt.Println("Tray autostart:   disabled")
	}
}

func printDaemonStatus(ctx context.Context) {
	fmt.Println("Background services:")

	webStatus, err := daemon.GetStatus("web", "Web server")
	printOneDaemonStatus("Web server", webStatus, err)

	printDiscordStatus(ctx, webStatus)

	trayStatus, err := daemon.GetStatus("tray", "Tray icon")
	printOneDaemonStatus("Tray icon", trayStatus, err)
}

func printOneDaemonStatus(name string, status daemon.Status, err error) {
	if err != nil {
		fmt.Printf("  %-16s error: %v\n", name, err)
		return
	}
	if status.Running {
		fmt.Printf("  %-16s running (pid %d, started %s)\n", name, status.PID, status.StartedAt.Format(time.RFC3339))
	} else {
		fmt.Printf("  %-16s not running\n", name)
	}
}

// printDiscordStatus queries the running core's own /botson/api/discord/status
// instead of a daemon state file -- Discord no longer runs as its own
// backgroundable process (see AGENTS.md's "Unified core architecture"), so
// there's no .pid file to check; it only has a status while a core is up.
func printDiscordStatus(ctx context.Context, webStatus daemon.Status) {
	const name = "Discord gateway"
	if !webStatus.Running {
		fmt.Printf("  %-16s not running (core isn't running)\n", name)
		return
	}

	apiPort := 8080
	if p, ok := webStatus.Meta["apiPort"]; ok {
		if port, err := strconv.Atoi(p); err == nil {
			apiPort = port
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client := apiclient.New(fmt.Sprintf("http://127.0.0.1:%d", apiPort))
	running, err := client.DiscordStatus(reqCtx)
	if err != nil {
		fmt.Printf("  %-16s error: %v\n", name, err)
		return
	}
	if running {
		fmt.Printf("  %-16s running (in-process within the core)\n", name)
	} else {
		fmt.Printf("  %-16s not running\n", name)
	}
}

func presence(ok bool) string {
	if ok {
		return "configured"
	}
	return "not set"
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func formatSize(n int64) string {
	const mb = 1024 * 1024
	if n >= mb {
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	}
	return fmt.Sprintf("%d bytes", n)
}
