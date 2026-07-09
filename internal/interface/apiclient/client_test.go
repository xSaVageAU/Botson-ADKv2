package apiclient

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"botsonv2/internal/interface/natscore"
)

// newTestAgent builds a minimal agent.Agent driven by a hand-written Run
// function instead of a real Gemini model, so these tests exercise the
// wire protocol without needing an API key.
func newTestAgent(t *testing.T, name string, run func(agent.InvocationContext) iter.Seq2[*session.Event, error]) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{Name: name, Run: run})
	if err != nil {
		t.Fatalf("failed to build test agent: %v", err)
	}
	return a
}

// startTestServer starts a bare embedded NATS server on an ephemeral
// loopback port with nothing subscribed yet, returning its client URL.
func startTestServer(t *testing.T) string {
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
	return srv.ClientURL()
}

// newTestClient starts an embedded NATS server, wires natscore.Serve to it
// using cfg, and returns a Client connected to the same server -- the test
// equivalent of a running `botson core` plus a TUI pointed at it.
func newTestClient(t *testing.T, cfg *launcher.Config) *Client {
	t.Helper()

	url := startTestServer(t)

	serverConn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect core-side NATS conn: %v", err)
	}
	t.Cleanup(serverConn.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go natscore.Serve(ctx, serverConn, cfg)

	client, err := New(url)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func textEvent(author, text string) *session.Event {
	return &session.Event{
		Author:      author,
		LLMResponse: model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: text}}}},
	}
}

func TestDefaultAgent(t *testing.T) {
	root := newTestAgent(t, "Agent Botson", nil)
	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(root),
		SessionService: session.InMemoryService(),
	}
	client := newTestClient(t, cfg)

	name, err := client.DefaultAgent(t.Context())
	if err != nil {
		t.Fatalf("DefaultAgent failed: %v", err)
	}
	if name != "Agent Botson" {
		t.Fatalf("expected %q, got %q", "Agent Botson", name)
	}
}

func TestCreateSession(t *testing.T) {
	root := newTestAgent(t, "Agent Botson", nil)
	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(root),
		SessionService: session.InMemoryService(),
	}
	client := newTestClient(t, cfg)

	id, err := client.CreateSession(t.Context(), "Agent Botson", "tui", "", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected a server-generated id, got an empty string")
	}
}

func TestGetSession(t *testing.T) {
	root := newTestAgent(t, "Agent Botson", nil)
	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(root),
		SessionService: session.InMemoryService(),
	}
	client := newTestClient(t, cfg)

	id, err := client.CreateSession(t.Context(), "Agent Botson", "tui", "sess-1", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	info, err := client.GetSession(t.Context(), "Agent Botson", "tui", id)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if info.State["k"] != "v" {
		t.Fatalf("unexpected state: %+v", info.State)
	}
}

func TestGetSessionMissingIsAnError(t *testing.T) {
	root := newTestAgent(t, "Agent Botson", nil)
	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(root),
		SessionService: session.InMemoryService(),
	}
	client := newTestClient(t, cfg)

	if _, err := client.GetSession(t.Context(), "Agent Botson", "tui", "missing"); err == nil {
		t.Fatal("expected an error for a nonexistent session, got none")
	}
}

func TestRunStreamsEvents(t *testing.T) {
	root := newTestAgent(t, "Agent Botson", func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			if !yield(textEvent("Agent Botson", "hello"), nil) {
				return
			}
			yield(textEvent("Agent Botson", " world"), nil)
		}
	})
	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(root),
		SessionService: session.InMemoryService(),
	}
	client := newTestClient(t, cfg)

	var texts []string
	for ev, err := range client.Run(t.Context(), "Agent Botson", "tui", "sess-1", &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}) {
		if err != nil {
			t.Fatalf("Run yielded an error: %v", err)
		}
		if ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}

	got := strings.Join(texts, "")
	if got != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", got)
	}
}

func TestRunSurfacesMidStreamErrorNotSilentTruncation(t *testing.T) {
	root := newTestAgent(t, "Agent Botson", func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			if !yield(textEvent("Agent Botson", "partial"), nil) {
				return
			}
			yield(nil, fmt.Errorf("boom"))
		}
	})
	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(root),
		SessionService: session.InMemoryService(),
	}
	client := newTestClient(t, cfg)

	var sawError bool
	for _, err := range client.Run(t.Context(), "Agent Botson", "tui", "sess-1", &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}) {
		if err != nil {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected a mid-stream agent error to surface as an error, got none")
	}
}

func TestRunNoResponderIsAnError(t *testing.T) {
	// No natscore.Serve running at all -- nothing is subscribed to
	// SubjectRun, matching "the core isn't running".
	url := startTestServer(t)
	client, err := New(url)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	t.Cleanup(client.Close)

	for _, err := range client.Run(t.Context(), "Nonexistent", "tui", "sess-1", &genai.Content{}) {
		if err == nil {
			t.Fatal("expected an error when nothing is listening on SubjectRun, got none")
		}
	}
}
