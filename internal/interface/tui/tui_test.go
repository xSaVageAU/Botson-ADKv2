package tui

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"botsonv2/internal/interface/apiclient"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nats-io/nats-server/v2/server"
)

// TestMain starts a real, headless Bubble Tea program for the whole test
// run and points the package-level `program` var at it, so that if a
// background goroutine spawned by Update (e.g. resumeAfterConfirmation)
// calls program.Send(...), it lands on a real running program instead of
// panicking on a nil *tea.Program. The tests below never inspect what
// that program does with those messages -- they only assert on the
// return value of calling Update directly.
func TestMain(m *testing.M) {
	p := tea.NewProgram(model{},
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignalHandler(),
	)
	program = p
	go p.Run() //nolint:errcheck

	code := m.Run()
	p.Quit()
	os.Exit(code)
}

// unreachableClient connects to a bare embedded NATS server with no
// natscore.Serve wired up to it, so any in-flight call a background
// goroutine makes gets a fast "no responders" error rather than hanging or
// actually reaching a real core.
func unreachableClient(t *testing.T) *apiclient.Client {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
	if err != nil {
		t.Fatalf("failed to start embedded NATS server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS server never became ready")
	}
	t.Cleanup(srv.Shutdown)

	client, err := apiclient.New(srv.ClientURL())
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestUpdateHitlPendingMsgShowsPrompt(t *testing.T) {
	m := model{client: unreachableClient(t), agentName: "Agent Botson", sessionID: "sess-1"}

	updated, _ := m.Update(hitlPendingMsg{callID: "call-1", toolName: "writeFile", hint: "writes a file"})
	mm := updated.(model)

	if mm.pendingHITL == nil {
		t.Fatal("expected pendingHITL to be set")
	}
	if mm.pendingHITL.toolName != "writeFile" || mm.pendingHITL.callID != "call-1" {
		t.Fatalf("unexpected pendingHITL: %+v", mm.pendingHITL)
	}
	if len(mm.history) == 0 || !strings.Contains(mm.history[len(mm.history)-1], "writeFile") {
		t.Fatalf("expected the pending confirmation to be rendered into history, got: %v", mm.history)
	}
}

func TestApprovingClearsHitlAndResumes(t *testing.T) {
	m := model{
		client:      unreachableClient(t),
		agentName:   "Agent Botson",
		sessionID:   "sess-1",
		pendingHITL: &hitlPendingMsg{callID: "call-1", toolName: "writeFile", hint: "writes a file"},
	}

	for _, key := range []string{"y", "enter"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if key == "enter" {
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		mm := updated.(model)
		if mm.pendingHITL != nil {
			t.Fatalf("key %q: expected pendingHITL to be cleared after approving, still set: %+v", key, mm.pendingHITL)
		}
		if !mm.thinking {
			t.Fatalf("key %q: expected thinking=true after approving (resuming the run)", key)
		}
		if len(mm.history) == 0 || !strings.Contains(mm.history[len(mm.history)-1], "Approved") {
			t.Fatalf("key %q: expected an 'Approved' line in history, got: %v", key, mm.history)
		}
	}
}

func TestDenyingClearsHitlAndResumes(t *testing.T) {
	m := model{
		client:      unreachableClient(t),
		agentName:   "Agent Botson",
		sessionID:   "sess-1",
		pendingHITL: &hitlPendingMsg{callID: "call-1", toolName: "writeFile", hint: "writes a file"},
	}

	for _, key := range []string{"n", "esc"} {
		var updated tea.Model
		if key == "esc" {
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		} else {
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
		mm := updated.(model)
		if mm.pendingHITL != nil {
			t.Fatalf("key %q: expected pendingHITL to be cleared after denying, still set: %+v", key, mm.pendingHITL)
		}
		if !mm.thinking {
			t.Fatalf("key %q: expected thinking=true after denying (resuming the run)", key)
		}
		if len(mm.history) == 0 || !strings.Contains(mm.history[len(mm.history)-1], "Denied") {
			t.Fatalf("key %q: expected a 'Denied' line in history, got: %v", key, mm.history)
		}
	}
}

func TestTextInputIgnoredWhilePending(t *testing.T) {
	m := model{
		client:      unreachableClient(t),
		agentName:   "Agent Botson",
		sessionID:   "sess-1",
		pendingHITL: &hitlPendingMsg{callID: "call-1", toolName: "writeFile"},
	}

	// Typing while a confirmation is pending must not be forwarded to the
	// text input, and must not be misread as an ordinary chat message.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	mm := updated.(model)
	if mm.textInput.Value() != "" {
		t.Fatalf("expected text input to be untouched while a confirmation is pending, got %q", mm.textInput.Value())
	}
	if mm.pendingHITL == nil {
		t.Fatal("an unrelated keypress must not clear a pending confirmation")
	}
}
