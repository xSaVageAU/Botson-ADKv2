package adkgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/Savs-Agents/NATS-ADK-Proxy/client"
)

func startTestNATS(t *testing.T) *nats.Conn {
	t.Helper()

	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   natsserver.RANDOM_PORT,
		NoLog:  true,
		NoSigs: true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("create test nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("test nats server not ready in time")
	}
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect to test nats server: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func TestGateway_RESTRoundTrip(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/echo" {
			t.Errorf("unexpected backend request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xyz" {
			t.Errorf("Authorization header not forwarded: got %q", got)
		}
		w.Header().Set("X-Reply", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	nc := startTestNATS(t)

	gw := newGateway(nc, backend.URL, gatewayOptions{})
	if err := gw.Start(); err != nil {
		t.Fatalf("gateway Start: %v", err)
	}
	defer gw.Close()

	c := client.New(nc)
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer xyz")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.Do(ctx, http.MethodPost, "/api/echo", hdr, []byte(`{"hi":"there"}`))
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	if resp.Status != http.StatusCreated {
		t.Errorf("Status = %d, want %d", resp.Status, http.StatusCreated)
	}
	if got := resp.Header.Get("X-Reply"); got != "yes" {
		t.Errorf("X-Reply header not round-tripped: got %q", got)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("Body = %q, want %q", resp.Body, `{"ok":true}`)
	}
}

func TestGateway_FixedRoutes(t *testing.T) {
	var gotMethod, gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("card"))
	}))
	defer backend.Close()

	nc := startTestNATS(t)
	gw := newGateway(nc, backend.URL, gatewayOptions{})
	if err := gw.Start(); err != nil {
		t.Fatalf("gateway Start: %v", err)
	}
	defer gw.Close()

	c := client.New(nc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.AgentCard(ctx); err != nil {
		t.Fatalf("AgentCard: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/.well-known/agent-card.json" {
		t.Errorf("agent card routed to %s %s, want GET /.well-known/agent-card.json", gotMethod, gotPath)
	}

	if _, err := c.A2AInvoke(ctx, true, []byte(`{}`)); err != nil {
		t.Fatalf("A2AInvoke(v1): %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/a2a/v1/invoke" {
		t.Errorf("a2a v1 routed to %s %s, want POST /a2a/v1/invoke", gotMethod, gotPath)
	}

	if _, err := c.A2AInvoke(ctx, false, []byte(`{}`)); err != nil {
		t.Fatalf("A2AInvoke(v0): %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/a2a/invoke" {
		t.Errorf("a2a v0 routed to %s %s, want POST /a2a/invoke", gotMethod, gotPath)
	}
}

func TestGateway_UnsupportedMethod(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called for an unsupported method")
	}))
	defer backend.Close()

	nc := startTestNATS(t)
	gw := newGateway(nc, backend.URL, gatewayOptions{})
	if err := gw.Start(); err != nil {
		t.Fatalf("gateway Start: %v", err)
	}
	defer gw.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// TRACE isn't in the forwarded method set but still matches the
	// "<prefix>.rest.*" subscription wildcard, so it must come back as a
	// gateway error rather than hang or panic.
	_, err := client.New(nc).Do(ctx, http.MethodTrace, "/api/list-apps", nil, nil)
	if err == nil {
		t.Fatal("expected an error for an unsupported method, got nil")
	}
}

func TestGateway_BackendTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	nc := startTestNATS(t)
	gw := newGateway(nc, backend.URL, gatewayOptions{requestTimeout: 20 * time.Millisecond})
	if err := gw.Start(); err != nil {
		t.Fatalf("gateway Start: %v", err)
	}
	defer gw.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.New(nc).Do(ctx, http.MethodGet, "/api/slow", nil, nil)
	if err == nil {
		t.Fatal("expected an error for a backend that exceeds RequestTimeout, got nil")
	}
}
