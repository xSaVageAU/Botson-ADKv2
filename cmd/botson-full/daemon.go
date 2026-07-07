package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
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
