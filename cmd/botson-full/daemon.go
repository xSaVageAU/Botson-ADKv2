package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"botsonv2/core/config"
)

// daemonState is the metadata persisted for a backgrounded subcommand so a
// later `start`/`stop`/`status` invocation (a separate process) can find and
// control the detached child. Port is a loopback control channel the child
// listens on -- needed because Windows has no way to deliver a graceful
// shutdown signal to an arbitrary detached process the way SIGTERM does on
// Unix, so `stop` always talks to it over that channel on every platform.
type daemonState struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

func daemonStatePath(name string) (string, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, name+".pid"), nil
}

func daemonLogPath(name string) (string, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(logDir, name+".log"), nil
}

func writeDaemonState(name string, state daemonState) error {
	path, err := daemonStatePath(name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readDaemonState(name string) (*daemonState, error) {
	path, err := daemonStatePath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state daemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func removeDaemonState(name string) error {
	path, err := daemonStatePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// daemonAlive reports whether the background process recorded in state is
// still up, by attempting to reach its local control channel.
func daemonAlive(state *daemonState) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", state.Port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// controlStopMessage is the only line that triggers a shutdown over the
// control channel. Plain connect-and-close probes (used by daemonAlive for
// liveness/readiness checks) must NOT trigger a shutdown, so the message
// content is checked rather than reacting to any successful connection.
const controlStopMessage = "stop"

// startControlListener opens a loopback control channel for a background
// process. Sending the line "stop" to it triggers a graceful shutdown by
// invoking cancel; any other (or empty/no) message is ignored, so a bare
// liveness probe never accidentally shuts the process down.
func startControlListener(cancel func()) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, _ := bufio.NewReader(conn).ReadString('\n')
			conn.Close()
			if strings.TrimSpace(line) == controlStopMessage {
				cancel()
			}
		}
	}()

	return ln, port, nil
}

// startDaemon spawns a detached background process running this same
// executable with childArgs, and waits (up to 5s) for it to report itself
// ready via the daemon state file. id is the internal name used for
// state/log file paths; displayName is what's shown to the user.
func startDaemon(id, displayName string, childArgs []string) error {
	if state, err := readDaemonState(id); err == nil && daemonAlive(state) {
		fmt.Printf("%s is already running (pid %d).\n", displayName, state.PID)
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	logPath, err := daemonLogPath(id)
	if err != nil {
		return fmt.Errorf("failed to resolve log path: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	child := exec.Command(exePath, childArgs...)
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

		if state, err := readDaemonState(id); err == nil && state.PID == child.Process.Pid && daemonAlive(state) {
			fmt.Printf("Started %s in background (pid %d).\nLogs: %s\n", displayName, state.PID, logPath)
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for %s to report ready (see %s)", displayName, logPath)
}

// stopDaemon asks a running background process to shut down gracefully over
// its loopback control channel, or force-kills it if force is true.
func stopDaemon(id, displayName string, force bool) error {
	state, err := readDaemonState(id)
	if err != nil {
		fmt.Printf("%s is not running.\n", displayName)
		return nil
	}

	if !daemonAlive(state) {
		_ = removeDaemonState(id)
		fmt.Printf("%s is not running (stale state cleared).\n", displayName)
		return nil
	}

	if force {
		if proc, err := os.FindProcess(state.PID); err == nil {
			_ = proc.Kill()
		}
		_ = removeDaemonState(id)
		fmt.Printf("Force-killed %s (pid %d).\n", displayName, state.PID)
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
			fmt.Printf("%s offline.\n", displayName)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("%s did not shut down within 10s; try --force", displayName)
}

// printDaemonStatus prints whether the named background process is running.
func printDaemonStatus(id, displayName string) error {
	state, err := readDaemonState(id)
	if err != nil {
		fmt.Printf("%s: not running\n", displayName)
		return nil
	}
	if !daemonAlive(state) {
		_ = removeDaemonState(id)
		fmt.Printf("%s: not running (stale state cleared)\n", displayName)
		return nil
	}
	fmt.Printf("%s: running (pid %d, started %s)\n", displayName, state.PID, state.StartedAt.Format(time.RFC3339))
	return nil
}
