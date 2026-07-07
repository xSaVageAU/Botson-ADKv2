package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"botsonv2/core/interface/discord"

	"github.com/spf13/cobra"
)

const discordDaemonName = "discord"

func newDiscordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discord",
		Short: "Start the standalone Discord gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscord(cmd.Context())
		},
	}

	cmd.AddCommand(newDiscordStartCmd(), newDiscordStopCmd(), newDiscordStatusCmd(), newDiscordDaemonChildCmd())
	return cmd
}

// noBootstrap skips the root command's expensive config/Gemini/agent/session
// bootstrap for subcommands that only manage a background process's
// lifecycle and never touch the agent runtime themselves.
func noBootstrap(cmd *cobra.Command, args []string) error { return nil }

// newDiscordDaemonChildCmd is the hidden entrypoint the detached background
// process actually runs; users invoke `start`/`stop`/`status` instead.
func newDiscordDaemonChildCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__daemon-child",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonCtx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			ln, port, err := startControlListener(cancel)
			if err != nil {
				return fmt.Errorf("failed to start control listener: %w", err)
			}
			defer ln.Close()

			if err := writeDaemonState(discordDaemonName, daemonState{
				PID:       os.Getpid(),
				Port:      port,
				StartedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("failed to write daemon state: %w", err)
			}
			defer removeDaemonState(discordDaemonName)

			return runDiscord(daemonCtx)
		},
	}
}

func newDiscordStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "start",
		Short:             "Start the Discord gateway as a detached background process",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startDiscordDaemon()
		},
	}
	return cmd
}

func startDiscordDaemon() error {
	if state, err := readDaemonState(discordDaemonName); err == nil && daemonAlive(state) {
		fmt.Printf("Discord gateway is already running (pid %d).\n", state.PID)
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	logPath, err := daemonLogPath(discordDaemonName)
	if err != nil {
		return fmt.Errorf("failed to resolve log path: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	child := exec.Command(exePath, "discord", "__daemon-child")
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = detachedSysProcAttr()

	if err := child.Start(); err != nil {
		return fmt.Errorf("failed to start background process: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("background process exited immediately: %w (see %s)", err, logPath)
			}
			return fmt.Errorf("background process exited immediately (see %s)", logPath)
		default:
		}

		if state, err := readDaemonState(discordDaemonName); err == nil && state.PID == child.Process.Pid && daemonAlive(state) {
			fmt.Printf("Started Discord gateway in background (pid %d).\nLogs: %s\n", state.PID, logPath)
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for background process to report ready (see %s)", logPath)
}

func newDiscordStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             "Stop the background Discord gateway",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDiscordDaemon(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force-kill the background process instead of asking it to shut down gracefully")
	return cmd
}

func stopDiscordDaemon(force bool) error {
	state, err := readDaemonState(discordDaemonName)
	if err != nil {
		fmt.Println("Discord gateway is not running.")
		return nil
	}

	if !daemonAlive(state) {
		_ = removeDaemonState(discordDaemonName)
		fmt.Println("Discord gateway is not running (stale state cleared).")
		return nil
	}

	if force {
		if proc, err := os.FindProcess(state.PID); err == nil {
			_ = proc.Kill()
		}
		_ = removeDaemonState(discordDaemonName)
		fmt.Printf("Force-killed Discord gateway (pid %d).\n", state.PID)
		return nil
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", state.Port), 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to reach background process for graceful shutdown: %w (try --force)", err)
	}
	fmt.Fprintf(conn, "%s\n", controlStopMessage)
	conn.Close()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !daemonAlive(state) {
			fmt.Println("Discord gateway offline.")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("gateway did not shut down within 10s; try --force")
}

func newDiscordStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Show whether the background Discord gateway is running",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := readDaemonState(discordDaemonName)
			if err != nil {
				fmt.Println("Discord gateway: not running")
				return nil
			}
			if !daemonAlive(state) {
				_ = removeDaemonState(discordDaemonName)
				fmt.Println("Discord gateway: not running (stale state cleared)")
				return nil
			}
			fmt.Printf("Discord gateway: running (pid %d, started %s)\n", state.PID, state.StartedAt.Format(time.RFC3339))
			return nil
		},
	}
}

func runDiscord(ctx context.Context) error {
	token := boot.Config.Discord.Token
	if token == "" {
		return fmt.Errorf("discord.token is not defined in ~/.botsonv2/config.json")
	}

	gateway, err := discord.New(token, boot.Launcher)
	if err != nil {
		return fmt.Errorf("failed to initialize Discord gateway: %w", err)
	}

	log.Println("Starting Discord Gateway...")
	if err := gateway.Start(); err != nil {
		return fmt.Errorf("failed to start Discord gateway: %w", err)
	}
	log.Println("Discord Gateway is online. Press Ctrl+C to terminate.")

	<-ctx.Done()

	log.Println("Shutting down Discord Gateway gracefully...")
	if err := gateway.Close(); err != nil {
		log.Printf("Error closing gateway connection: %v", err)
	}
	log.Println("Discord Gateway offline. Good bye!")
	return nil
}
