//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/getlantern/systray"
	"github.com/spf13/cobra"
)

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
			return startDaemon(trayDaemonName, trayDisplayName, []string{"tray", "__daemon-child"})
		},
	}
}

func newTrayStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the background tray icon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDaemon(trayDaemonName, trayDisplayName, force)
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
			return printDaemonStatus(trayDaemonName, trayDisplayName)
		},
	}
}

// runTray registers this process in the same daemon state/control-channel
// system discord and web use, so `tray status`/`tray stop` work regardless
// of whether the tray was launched in the foreground or via `tray start`.
func runTray(ctx context.Context) error {
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ln, port, err := startControlListener(cancel)
	if err != nil {
		return fmt.Errorf("failed to start control listener: %w", err)
	}
	defer ln.Close()

	if err := writeDaemonState(trayDaemonName, daemonState{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("failed to write daemon state: %w", err)
	}
	defer removeDaemonState(trayDaemonName)

	go func() {
		<-daemonCtx.Done()
		systray.Quit()
	}()

	systray.Run(onTrayReady, onTrayExit)
	return nil
}

// trayService is a tray-side view of a backgroundable command (discord, web).
// The tray is just another process talking to the same daemon state/control
// files the CLI uses -- it doesn't own or supervise these processes' lifecycle.
type trayService struct {
	id          string
	displayName string
	childArgs   []string
	toggle      *systray.MenuItem
	running     bool
}

var traySvcDiscord = &trayService{
	id:          discordDaemonName,
	displayName: "Discord",
	childArgs:   []string{"discord", "__daemon-child"},
}

var traySvcWeb = &trayService{
	id:          webDaemonName,
	displayName: "Web",
	childArgs:   webDaemonChildArgs(8080, false, false),
}

func onTrayReady() {
	systray.SetIcon(trayIconData)
	systray.SetTooltip("Botson background services")

	traySvcDiscord.toggle = systray.AddMenuItem("Start Discord", "Start/stop the Discord gateway")
	traySvcWeb.toggle = systray.AddMenuItem("Start Web", "Start/stop the web console")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Close this tray icon (background services keep running)")
	mStopAllQuit := systray.AddMenuItem("Stop All && Quit", "Stop Discord and Web, then close this tray icon")

	go trayPollLoop()

	go func() {
		for range traySvcDiscord.toggle.ClickedCh {
			toggleTrayService(traySvcDiscord)
		}
	}()
	go func() {
		for range traySvcWeb.toggle.ClickedCh {
			toggleTrayService(traySvcWeb)
		}
	}()
	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
	go func() {
		<-mStopAllQuit.ClickedCh
		_ = stopDaemon(traySvcDiscord.id, traySvcDiscord.displayName, false)
		_ = stopDaemon(traySvcWeb.id, traySvcWeb.displayName, false)
		systray.Quit()
	}()
}

func onTrayExit() {}

func trayPollLoop() {
	refreshTrayService(traySvcDiscord)
	refreshTrayService(traySvcWeb)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		refreshTrayService(traySvcDiscord)
		refreshTrayService(traySvcWeb)
	}
}

func refreshTrayService(svc *trayService) {
	state, err := readDaemonState(svc.id)
	svc.running = err == nil && daemonAlive(state)
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
		_ = stopDaemon(svc.id, svc.displayName, false)
	} else {
		_ = startDaemon(svc.id, svc.displayName, svc.childArgs)
	}
	refreshTrayService(svc)
}
