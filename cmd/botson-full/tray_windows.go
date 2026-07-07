//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"time"

	"github.com/getlantern/systray"
	"github.com/spf13/cobra"
)

//go:embed assets/tray.ico
var trayIconData []byte

func newTrayCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "tray",
		Short:             "Show a system tray icon to monitor and control background services",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			systray.Run(onTrayReady, onTrayExit)
			return nil
		},
	}
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
