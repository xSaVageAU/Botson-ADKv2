// Package apiclient is a thin NATS client for Botson's shared core -- the
// one true way any process (this repo's TUI, and future standalone
// Discord/web projects) talks to a running core, per the subjects and wire
// types internal/interface/natscore defines. It exists so an interface
// like the TUI doesn't need to bootstrap its own copy of the Gemini
// model/agent loader/session service; it just talks to whichever core is
// already running.
package apiclient

import (
	"context"
	"encoding/json"
	"fmt"

	"botsonv2/internal/interface/natscore"

	"github.com/nats-io/nats.go"
)

// Client talks to one core instance over NATS.
type Client struct {
	nc *nats.Conn
}

// New connects to the core's embedded NATS server at natsURL, e.g.
// "nats://127.0.0.1:4222".
func New(natsURL string) (*Client, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to reach core: %w", err)
	}
	return &Client{nc: nc}, nil
}

// Close releases the underlying NATS connection.
func (c *Client) Close() {
	c.nc.Close()
}

// wrapRequestErr standardizes the "couldn't get a reply at all" case
// (no responders, timeout, connection closed, context cancelled) across
// every request/reply call this client makes.
func wrapRequestErr(err error) error {
	return fmt.Errorf("failed to reach core: %w", err)
}

// DefaultAgent asks the core which agent name is the configured root agent.
func (c *Client) DefaultAgent(ctx context.Context) (string, error) {
	msg, err := c.nc.RequestWithContext(ctx, natscore.SubjectDefaultAgent, nil)
	if err != nil {
		return "", wrapRequestErr(err)
	}

	var out natscore.DefaultAgentReply
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		return "", fmt.Errorf("failed to decode default agent response: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("core returned an error for default agent: %s", out.Error)
	}
	return out.Name, nil
}

// CreateSession asks the core to create a session. If sessionID is empty,
// the core generates one and its value is returned; otherwise the same
// sessionID passed in is returned unchanged.
func (c *Client) CreateSession(ctx context.Context, appName, userID, sessionID string, state map[string]any) (string, error) {
	body, err := json.Marshal(natscore.SessionCreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		State:     state,
	})
	if err != nil {
		return "", err
	}

	msg, err := c.nc.RequestWithContext(ctx, natscore.SubjectSessionCreate, body)
	if err != nil {
		return "", wrapRequestErr(err)
	}

	var out natscore.SessionCreateReply
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		return "", fmt.Errorf("failed to decode create-session response: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("core returned an error creating session: %s", out.Error)
	}
	return out.ID, nil
}

// GetSession fetches a session's stored events and state.
func (c *Client) GetSession(ctx context.Context, appName, userID, sessionID string) (*SessionInfo, error) {
	body, err := json.Marshal(natscore.SessionGetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}

	msg, err := c.nc.RequestWithContext(ctx, natscore.SubjectSessionGet, body)
	if err != nil {
		return nil, wrapRequestErr(err)
	}

	var out natscore.SessionGetReply
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		return nil, fmt.Errorf("failed to decode session response: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("core returned an error fetching session: %s", out.Error)
	}
	return &out.SessionInfo, nil
}
