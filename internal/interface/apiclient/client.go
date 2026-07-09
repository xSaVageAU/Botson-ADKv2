// Package apiclient is a thin HTTP client for Botson's shared core -- the
// REST API a running `botson web` process already serves (mostly ADK's
// own, plus Botson's small additions under /botson/api/). It exists so an
// interface like the TUI doesn't need to bootstrap its own copy of the
// Gemini model/agent loader/session service; it just talks to whichever
// core is already running.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/genai"
)

// Client talks to one core instance over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client talking to the core at baseURL, e.g.
// "http://127.0.0.1:8080".
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{}, // no Timeout: /api/run_sse is a long-lived streaming response
	}
}

// DefaultAgent calls GET /botson/api/default-agent to learn which agent
// name is the configured root agent.
func (c *Client) DefaultAgent(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/botson/api/default-agent", nil)
	if err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("core returned %s for default agent: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode default agent response: %w", err)
	}
	return out.Name, nil
}

// CreateSession calls POST /api/apps/{app}/users/{user}/sessions[/{id}].
// If sessionID is empty, the core generates one and its value is
// returned; otherwise the same sessionID passed in is returned unchanged.
func (c *Client) CreateSession(ctx context.Context, appName, userID, sessionID string, state map[string]any) (string, error) {
	reqURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions", c.baseURL, url.PathEscape(appName), url.PathEscape(userID))
	if sessionID != "" {
		reqURL += "/" + url.PathEscape(sessionID)
	}

	body, err := json.Marshal(map[string]any{"state": state, "events": []any{}})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("core returned %s creating session: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode create-session response: %w", err)
	}
	return out.ID, nil
}

// SessionInfo mirrors the subset of ADK's REST session response a caller
// needs to resume a prior conversation: its stored events (to replay into
// a fresh transcript) and state (which carries e.g. __session_metadata__).
type SessionInfo struct {
	Events []Event        `json:"events"`
	State  map[string]any `json:"state"`
}

// GetSession calls GET /api/apps/{app}/users/{user}/sessions/{id}. Note
// ADK's own handler returns 500, not 404, when the session doesn't exist
// under that app/user pair -- callers should treat any non-200 here as
// "no such session for this app/user", not just 404.
func (c *Client) GetSession(ctx context.Context, appName, userID, sessionID string) (*SessionInfo, error) {
	reqURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", c.baseURL, url.PathEscape(appName), url.PathEscape(userID), url.PathEscape(sessionID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("core returned %s fetching session: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode session response: %w", err)
	}
	return &out, nil
}

// Run POSTs to /api/run_sse and streams back decoded Events, shaped like
// runner.Runner.Run's own iterator so callers built against that need
// minimal changes.
func (c *Client) Run(ctx context.Context, appName, userID, sessionID string, msg *genai.Content) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		body, err := json.Marshal(map[string]any{
			"appName":    appName,
			"userId":     userID,
			"sessionId":  sessionID,
			"newMessage": msg,
		})
		if err != nil {
			yield(nil, err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/run_sse", bytes.NewReader(body))
		if err != nil {
			yield(nil, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			yield(nil, fmt.Errorf("failed to reach core: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("core returned %s: %s", resp.Status, strings.TrimSpace(string(data))))
			return
		}

		for ev, err := range parseSSE(ctx, resp.Body) {
			if !yield(ev, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}
