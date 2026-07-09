//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"botsonv2/internal/config"
	"botsonv2/internal/daemon"

	"github.com/getlantern/systray"
	"github.com/spf13/cobra"
)

// trayWorkspaceDir resolves the working directory for a process tray
// itself spawns. Tray typically has no meaningful cwd of its own (e.g.
// launched via a login-time autostart entry with no real working
// directory), so it prefers the explicitly configured workspace over its
// own ambient cwd, falling back to that cwd only if nothing's configured.
func trayWorkspaceDir() string {
	if cfg, err := config.Load(); err == nil && cfg.WorkspaceDir != "" {
		return cfg.WorkspaceDir
	}
	wd, _ := os.Getwd()
	return wd
}

//go:embed assets/tray.ico
var trayIconData []byte

const trayDaemonName = "tray"
const trayDisplayName = "Tray icon"

func newTrayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "tray",
		Short:             "Show a system tray icon to monitor and control background services",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTray(cmd.Context())
		},
	}
	cmd.AddCommand(newTrayStartCmd(), newTrayStopCmd(), newTrayStatusCmd(), newTrayDaemonChildCmd())
	return cmd
}

// newTrayDaemonChildCmd is the hidden entrypoint the detached background
// process actually runs; users invoke `tray start` instead.
func newTrayDaemonChildCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__daemon-child",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTray(cmd.Context())
		},
	}
}

func newTrayStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the tray icon as a detached background process (no console window)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, logPath, err := daemon.Start(trayDaemonName, trayDisplayName, trayWorkspaceDir(), []string{"tray", "__daemon-child"})
			if err != nil {
				return err
			}
			fmt.Printf("Started %s in background (pid %d).\nLogs: %s\n", trayDisplayName, pid, logPath)
			return nil
		},
	}
}

func newTrayStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the background tray icon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Stop(trayDaemonName, trayDisplayName, force); err != nil {
				return err
			}
			fmt.Printf("%s offline.\n", trayDisplayName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force-kill instead of asking it to quit gracefully")
	return cmd
}

func newTrayStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the tray icon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := daemon.GetStatus(trayDaemonName, trayDisplayName)
			if err != nil {
				return err
			}
			if !status.Running {
				fmt.Printf("%s: not running\n", trayDisplayName)
				return nil
			}
			fmt.Printf("%s: running (pid %d, started %s)\n", trayDisplayName, status.PID, status.StartedAt.Format(time.RFC3339))
			return nil
		},
	}
}

// runTray registers this process in the same daemon state/control-channel
// system discord and web use, so `tray status`/`tray stop` work regardless
// of whether the tray was launched in the foreground or via `tray start`.
func runTray(ctx context.Context) error {
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ln, port, err := daemon.StartControlListener(cancel)
	if err != nil {
		return fmt.Errorf("failed to start control listener: %w", err)
	}
	defer ln.Close()

	if err := daemon.WriteState(trayDaemonName, daemon.State{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("failed to write daemon state: %w", err)
	}
	defer daemon.RemoveState(trayDaemonName)

	go func() {
		<-daemonCtx.Done()
		systray.Quit()
	}()

	systray.Run(onTrayReady, onTrayExit)
	return nil
}

// trayService is a tray-side view of a backgroundable command (just
// `core` now -- Discord and the web console no longer run as part of this
// binary, see AGENTS.md's "Unified core architecture"). The tray is just
// another process talking to the same daemon state/control files the CLI
// uses -- it doesn't own or supervise these processes' lifecycle.
type trayService struct {
	id          string
	displayName string
	childArgs   []string
	toggle      *systray.MenuItem
	running     bool
}

var traySvcCore = &trayService{
	id:          coreDaemonName,
	displayName: coreDisplayName,
	childArgs:   coreDaemonChildArgs(4222),
}

func onTrayReady() {
	systray.SetIcon(trayIconData)
	systray.SetTooltip("Botson background services")

	mOpenChat := systray.AddMenuItem("Open Chat", "Open a new terminal chat session")
	systray.AddSeparator()
	traySvcCore.toggle = systray.AddMenuItem("Start Core", "Start/stop Botson's shared core")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Close this tray icon (background services keep running)")
	mStopAllQuit := systray.AddMenuItem("Stop All && Quit", "Stop the core, then close this tray icon")

	go trayPollLoop()

	go func() {
		for range mOpenChat.ClickedCh {
			openChatWindow()
		}
	}()
	go func() {
		for range traySvcCore.toggle.ClickedCh {
			toggleTrayService(traySvcCore)
		}
	}()
	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
	go func() {
		<-mStopAllQuit.ClickedCh
		_ = daemon.Stop(traySvcCore.id, traySvcCore.displayName, false)
		systray.Quit()
	}()
}

// CREATE_NO_WINDOW isn't exposed as a named constant in the syscall
// package (same situation as DETACHED_PROCESS in internal/daemon), so its
// documented raw value is used directly here.
const createNoWindow = 0x08000000

// openChatWindow launches a new, visible console window running the TUI
// chat client, via `cmd /C start` rather than spawning the exe directly
// with CREATE_NEW_CONSOLE. exec.Command defaults Stdin/Stdout/Stderr to
// the null device when left unset, and that override wins even over a
// freshly allocated console -- the window appears but the process's I/O
// is wired to nowhere, so it looks blank and doesn't respond to input.
// `start` allocates the new console itself and connects the target
// process's I/O to it properly. CREATE_NO_WINDOW here just hides the
// momentary intermediate cmd.exe window; it has no effect on the new
// console `start` opens for the actual TUI process.
func openChatWindow() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command("cmd", "/C", "start", "Botson Chat", exePath, "tui")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
	}
	_ = cmd.Start()
}

func onTrayExit() {}

func trayPollLoop() {
	refreshTrayService(traySvcCore)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		refreshTrayService(traySvcCore)
	}
}

func refreshTrayService(svc *trayService) {
	status, err := daemon.GetStatus(svc.id, svc.displayName)
	svc.running = err == nil && status.Running
	if svc.toggle == nil {
		return
	}
	if svc.running {
		svc.toggle.SetTitle(fmt.Sprintf("Stop %s", svc.displayName))
	} else {
		svc.toggle.SetTitle(fmt.Sprintf("Start %s", svc.displayName))
	}
}

func toggleTrayService(svc *trayService) {
	if svc.running {
		_ = daemon.Stop(svc.id, svc.displayName, false)
	} else {
		_, _, _ = daemon.Start(svc.id, svc.displayName, trayWorkspaceDir(), svc.childArgs)
	}
	refreshTrayService(svc)
}
