package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helperEnvVar, when set in the environment, makes
// TestStartSetsWorkingDirectory act as a helper subprocess instead of the
// test orchestrator -- the standard Go pattern for testing real child-
// process behavior (the same technique os/exec's own tests use), since
// Start's readiness protocol requires the child to actually open a
// control listener and write real daemon state, not just print a value.
const helperEnvVar = "BOTSON_DAEMON_TEST_HELPER"

// helperCwdFileEnvVar names the file the helper subprocess reports its
// own real os.Getwd() to, proving Start's dir parameter was actually
// applied at the OS level rather than merely accepted as a field.
const helperCwdFileEnvVar = "BOTSON_DAEMON_TEST_CWD_FILE"

func TestStartSetsWorkingDirectory(t *testing.T) {
	if os.Getenv(helperEnvVar) == "1" {
		runHelperDaemon(t)
		return
	}

	t.Setenv("HOME", t.TempDir()) // config.GetDataDir() resolves state/log paths under $HOME

	workDir := t.TempDir()
	resultDir := t.TempDir()
	cwdFile := filepath.Join(resultDir, "cwd.txt")

	t.Setenv(helperEnvVar, "1")
	t.Setenv(helperCwdFileEnvVar, cwdFile)

	const id = "test-workdir-daemon"
	_, _, err := Start(id, "Test Daemon", workDir, []string{
		"-test.run=^TestStartSetsWorkingDirectory$",
		"-test.v",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer Stop(id, "Test Daemon", true)

	reportedCwd, err := os.ReadFile(cwdFile)
	if err != nil {
		t.Fatalf("failed to read helper's reported cwd: %v", err)
	}

	wantDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("failed to resolve workDir: %v", err)
	}
	gotDir, err := filepath.EvalSymlinks(string(reportedCwd))
	if err != nil {
		t.Fatalf("failed to resolve reported cwd %q: %v", reportedCwd, err)
	}
	if gotDir != wantDir {
		t.Fatalf("child's real working directory was %q, want %q (Start's dir parameter was not applied)", gotDir, wantDir)
	}
}

// runHelperDaemon is what actually executes inside the spawned child
// process. It participates in the real Start/Stop protocol (a control
// listener + WriteState) so the orchestrating test's Start(...) call
// genuinely waits for and confirms this process is ready, then reports
// its own real os.Getwd() before blocking until told to stop.
func runHelperDaemon(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("helper: os.Getwd failed: %v", err)
	}
	if err := os.WriteFile(os.Getenv(helperCwdFileEnvVar), []byte(cwd), 0644); err != nil {
		t.Fatalf("helper: failed to write cwd file: %v", err)
	}

	done := make(chan struct{})
	ln, port, err := StartControlListener(func() { close(done) })
	if err != nil {
		t.Fatalf("helper: StartControlListener failed: %v", err)
	}
	defer ln.Close()

	if err := WriteState("test-workdir-daemon", State{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("helper: WriteState failed: %v", err)
	}
	defer RemoveState("test-workdir-daemon")

	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

func TestStateRoundTripsMeta(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const id = "test-meta-daemon"
	want := State{
		PID:       1234,
		Port:      5678,
		StartedAt: time.Now().Truncate(time.Second),
		Meta:      map[string]string{"apiPort": "8080"},
	}
	if err := WriteState(id, want); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}
	defer RemoveState(id)

	got, err := readState(id)
	if err != nil {
		t.Fatalf("readState failed: %v", err)
	}
	if got.Meta["apiPort"] != "8080" {
		t.Fatalf("expected Meta[apiPort]=8080, got %q", got.Meta["apiPort"])
	}
}
