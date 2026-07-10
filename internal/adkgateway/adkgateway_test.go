package adkgateway

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"

	"github.com/Savs-Agents/NATS-ADK-Proxy/client"
)

// newEchoAgent returns a deterministic, no-LLM agent.Agent that echoes back
// the caller's latest "user" message, so this test doesn't depend on a real
// model backend or API key.
func newEchoAgent(t *testing.T) agent.Agent {
	t.Helper()

	a, err := agent.New(agent.Config{
		Name:        "echo_agent",
		Description: "test echo agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				text := "(no input)"
				if sess := ctx.Session(); sess != nil {
					for e := range sess.Events().All() {
						if e.Author == "user" && e.Content != nil && len(e.Content.Parts) > 0 && e.Content.Parts[0].Text != "" {
							text = e.Content.Parts[0].Text
						}
					}
				}
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = ctx.Agent().Name()
				ev.Content = genai.NewContentFromText("echo: "+text, genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("create echo agent: %v", err)
	}
	return a
}

// runAgentRequest mirrors adk-go's internal models.RunAgentRequest JSON
// shape (that type is unexported outside the ADK module, so we redeclare the
// wire-compatible subset we need).
type runAgentRequest struct {
	AppName    string        `json:"appName"`
	UserID     string        `json:"userId"`
	SessionID  string        `json:"sessionId"`
	NewMessage genai.Content `json:"newMessage"`
}

func TestGateway_EndToEnd(t *testing.T) {
	nc := startTestNATS(t)
	echoAgent := newEchoAgent(t)

	p, err := New(Config{
		NATSConn: nc,
		ADK: launcher.Config{
			AgentLoader:    agent.NewSingleLoader(echoAgent),
			SessionService: session.InMemoryService(),
		},
	})
	if err != nil {
		t.Fatalf("adkgateway.New: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- p.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("Gateway.Run returned error on shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Gateway.Run did not shut down in time")
		}
	})

	c := client.New(nc, client.WithTimeout(5*time.Second))
	ctx, cancelReq := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelReq()

	// The backend + gateway startup happens inside Run(); poll the first
	// call until it succeeds instead of guessing a fixed sleep.
	var listResp *client.Response
	deadline := time.Now().Add(15 * time.Second)
	for {
		listResp, err = c.Do(ctx, http.MethodGet, "/api/list-apps", nil, nil)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway never became ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if listResp.Status != http.StatusOK {
		t.Fatalf("GET /api/list-apps status = %d, want 200; body=%s", listResp.Status, listResp.Body)
	}
	if !strings.Contains(string(listResp.Body), "echo_agent") {
		t.Errorf("list-apps body = %s, want it to contain %q", listResp.Body, "echo_agent")
	}

	sessResp, err := c.Do(ctx, http.MethodPost, "/api/apps/echo_agent/users/testuser/sessions/testsession", nil, nil)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sessResp.Status != http.StatusOK {
		t.Fatalf("create session status = %d, want 200; body=%s", sessResp.Status, sessResp.Body)
	}

	reqBody, err := json.Marshal(runAgentRequest{
		AppName:    "echo_agent",
		UserID:     "testuser",
		SessionID:  "testsession",
		NewMessage: *genai.NewContentFromText("hello there", genai.RoleUser),
	})
	if err != nil {
		t.Fatalf("marshal run request: %v", err)
	}

	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	runResp, err := c.Do(ctx, http.MethodPost, "/api/run", hdr, reqBody)
	if err != nil {
		t.Fatalf("run agent: %v", err)
	}
	if runResp.Status != http.StatusOK {
		t.Fatalf("run agent status = %d, want 200; body=%s", runResp.Status, runResp.Body)
	}
	if !strings.Contains(string(runResp.Body), "echo: hello there") {
		t.Errorf("run response = %s, want it to contain %q", runResp.Body, "echo: hello there")
	}

	cardResp, err := c.AgentCard(ctx)
	if err != nil {
		t.Fatalf("agent card: %v", err)
	}
	if !strings.Contains(string(cardResp.Body), "echo_agent") {
		t.Errorf("agent card body = %s, want it to contain %q", cardResp.Body, "echo_agent")
	}
}
