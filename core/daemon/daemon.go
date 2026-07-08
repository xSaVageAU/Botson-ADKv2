// Package daemon provides a generic detach/control lifecycle for
// backgroundable subcommands (discord, web, tray) of a single executable.
// It is deliberately CLI/HTTP-agnostic -- callers (Cobra subcommands, HTTP
// handlers) format Status/errors for their own audience.
package daemon

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

// State is the metadata persisted for a backgrounded subcommand so a later
// Start/Stop/GetStatus call (a separate process) can find and control the
// detached child. Port is a loopback control channel the child listens on
// -- needed because Windows has no way to deliver a graceful shutdown
// signal to an arbitrary detached process the way SIGTERM does on Unix, so
// Stop always talks to it over that channel on every platform.
type State struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
	// Meta carries caller-defined key/value pairs alongside the state this
	// package already tracks -- kept deliberately CLI/HTTP-agnostic (a free-
	// form string map, not a named field like "APIPort") so this package
	// doesn't need to know what an API port is. The web daemon stashes its
	// real REST API port here so another process can discover it.
	Meta map[string]string `json:"meta,omitempty"`
}

// Status is a caller-friendly snapshot of a daemon's current state.
type Status struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	Running     bool              `json:"running"`
	PID         int               `json:"pid,omitempty"`
	StartedAt   time.Time         `json:"startedAt,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

func statePath(id string) (string, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, id+".pid"), nil
}

// LogPath returns the path background output for id is written to.
func LogPath(id string) (string, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(logDir, id+".log"), nil
}

// WriteState persists a daemon's state; called by the detached child itself
// once it's ready to be discovered by Start/Stop/GetStatus.
func WriteState(id string, state State) error {
	path, err := statePath(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readState(id string) (*State, error) {
	path, err := statePath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// RemoveState deletes a daemon's persisted state; called by the detached
// child on graceful shutdown, and opportunistically by Stop/GetStatus when
// they find stale state pointing at a process that's no longer alive.
func RemoveState(id string) error {
	path, err := statePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// isAlive reports whether the background process recorded in state is still
// up, by attempting to reach its local control channel.
func isAlive(state *State) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", state.Port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// stopMessage is the only line that triggers a shutdown over the control
// channel. Plain connect-and-close probes (used by isAlive for
// liveness/readiness checks) must NOT trigger a shutdown, so the message
// content is checked rather than reacting to any successful connection.
const stopMessage = "stop"

// StartControlListener opens a loopback control channel for a background
// process. Sending the line "stop" to it triggers a graceful shutdown by
// invoking cancel; any other (or empty/no) message is ignored, so a bare
// liveness probe never accidentally shuts the process down. Called by a
// daemon's own detached-child entrypoint.
func StartControlListener(cancel func()) (net.Listener, int, error) {
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
			if strings.TrimSpace(line) == stopMessage {
				cancel()
			}
		}
	}()

	return ln, port, nil
}

// Start spawns a detached background process running this same executable
// with childArgs, and waits (up to 5s) for it to report itself ready via
// its state file. id is the internal name used for state/log file paths;
// displayName is only used in returned error text. dir sets the child's
// working directory explicitly -- callers should always pass their own
// os.Getwd() (or an intentionally configured workspace) rather than
// leaving this to whatever the child would otherwise inherit, which is
// silent and often surprising for a detached process (e.g. one launched
// by a login-time autostart entry with no meaningful cwd of its own).
func Start(id, displayName, dir string, childArgs []string) (pid int, logPath string, err error) {
	if state, err := readState(id); err == nil && isAlive(state) {
		return state.PID, "", fmt.Errorf("%s is already running (pid %d)", displayName, state.PID)
	}

	exePath, err := os.Executable()
	if err != nil {
		return 0, "", fmt.Errorf("failed to resolve executable path: %w", err)
	}

	logPath, err = LogPath(id)
	if err != nil {
		return 0, "", fmt.Errorf("failed to resolve log path: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, "", fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	child := exec.Command(exePath, childArgs...)
	child.Dir = dir
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = detachedSysProcAttr()

	if err := child.Start(); err != nil {
		return 0, "", fmt.Errorf("failed to start background process: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			if err != nil {
				return 0, "", fmt.Errorf("background process exited immediately: %w (see %s)", err, logPath)
			}
			return 0, "", fmt.Errorf("background process exited immediately (see %s)", logPath)
		default:
		}

		if state, err := readState(id); err == nil && state.PID == child.Process.Pid && isAlive(state) {
			return state.PID, logPath, nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	return 0, "", fmt.Errorf("timed out waiting for %s to report ready (see %s)", displayName, logPath)
}

// Stop asks a running background process to shut down gracefully over its
// loopback control channel, or force-kills it if force is true. Stopping an
// already-stopped daemon is not an error.
func Stop(id, displayName string, force bool) error {
	state, err := readState(id)
	if err != nil {
		return nil // not running
	}

	if !isAlive(state) {
		_ = RemoveState(id)
		return nil // stale state cleared
	}

	if force {
		if proc, err := os.FindProcess(state.PID); err == nil {
			_ = proc.Kill()
		}
		_ = RemoveState(id)
		return nil
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", state.Port), 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to reach background process for graceful shutdown: %w (try force)", err)
	}
	fmt.Fprintf(conn, "%s\n", stopMessage)
	conn.Close()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !isAlive(state) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("%s did not shut down within 10s; try force", displayName)
}

// GetStatus reports whether the named background process is running. A
// "not running" state (including a stale, cleaned-up state file) is a
// normal result, not an error; the returned error is only for unexpected
// failures resolving paths.
func GetStatus(id, displayName string) (Status, error) {
	state, err := readState(id)
	if err != nil {
		return Status{ID: id, DisplayName: displayName}, nil
	}
	if !isAlive(state) {
		_ = RemoveState(id)
		return Status{ID: id, DisplayName: displayName}, nil
	}
	return Status{
		ID:          id,
		DisplayName: displayName,
		Running:     true,
		PID:         state.PID,
		StartedAt:   state.StartedAt,
		Meta:        state.Meta,
	}, nil
}
