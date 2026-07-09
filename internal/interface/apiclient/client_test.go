package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestDefaultAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botson/api/default-agent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"name":"Agent Botson"}`)
	}))
	defer srv.Close()

	name, err := New(srv.URL).DefaultAgent(context.Background())
	if err != nil {
		t.Fatalf("DefaultAgent failed: %v", err)
	}
	if name != "Agent Botson" {
		t.Fatalf("expected %q, got %q", "Agent Botson", name)
	}
}

func TestCreateSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/apps/Agent%20Botson/users/tui/sessions"
		if r.URL.EscapedPath() != wantPath {
			t.Fatalf("expected path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		fmt.Fprint(w, `{"id":"generated-id-123"}`)
	}))
	defer srv.Close()

	id, err := New(srv.URL).CreateSession(context.Background(), "Agent Botson", "tui", "", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if id != "generated-id-123" {
		t.Fatalf("expected server-generated id, got %q", id)
	}
}

func TestGetSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/apps/Agent%20Botson/users/tui/sessions/sess-1"
		if r.URL.EscapedPath() != wantPath {
			t.Fatalf("expected path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		fmt.Fprint(w, `{"events":[{"author":"user","content":{"role":"user","parts":[{"text":"hi"}]}}],"state":{"k":"v"}}`)
	}))
	defer srv.Close()

	info, err := New(srv.URL).GetSession(context.Background(), "Agent Botson", "tui", "sess-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if len(info.Events) != 1 || info.Events[0].Author != "user" {
		t.Fatalf("unexpected events: %+v", info.Events)
	}
	if info.State["k"] != "v" {
		t.Fatalf("unexpected state: %+v", info.State)
	}
}

func TestGetSessionNonOKStatusIsAnError(t *testing.T) {
	// ADK's own handler returns 500, not 404, for "no such session" --
	// GetSession must still surface it as an error either way.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "session not found", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).GetSession(context.Background(), "Agent Botson", "tui", "missing"); err == nil {
		t.Fatal("expected an error for a non-200 response, got none")
	}
}

func TestRunStreamsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/run_sse" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"author":"Agent Botson","content":{"role":"model","parts":[{"text":"hello"}]}}`+"\n")
		fmt.Fprint(w, `data: {"author":"Agent Botson","content":{"role":"model","parts":[{"text":" world"}]}}`+"\n")
	}))
	defer srv.Close()

	var texts []string
	for ev, err := range New(srv.URL).Run(context.Background(), "Agent Botson", "tui", "sess-1", &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"author":"Agent Botson","content":{"role":"model","parts":[{"text":"partial"}]}}`+"\n")
		// Malformed JSON simulates a broken/truncated frame -- this must
		// surface as an error through the iterator, not be silently
		// dropped (which would look identical to a clean end of stream).
		fmt.Fprint(w, `data: {not valid json`+"\n")
	}))
	defer srv.Close()

	var sawError bool
	for _, err := range New(srv.URL).Run(context.Background(), "Agent Botson", "tui", "sess-1", &genai.Content{}) {
		if err != nil {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected a malformed SSE frame to surface as an error, got none")
	}
}

func TestRunNonOKStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such app", http.StatusNotFound)
	}))
	defer srv.Close()

	for _, err := range New(srv.URL).Run(context.Background(), "Nonexistent", "tui", "sess-1", &genai.Content{}) {
		if err == nil {
			t.Fatal("expected an error for a non-200 response, got none")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Fatalf("expected the error to mention the status, got: %v", err)
		}
	}
}
