package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DiscordStart calls POST /botson/api/discord/start, starting the Discord
// gateway within the core.
func (c *Client) DiscordStart(ctx context.Context) error {
	return c.postBotsonAPI(ctx, "/botson/api/discord/start")
}

// DiscordStop calls POST /botson/api/discord/stop, stopping the Discord
// gateway within the core.
func (c *Client) DiscordStop(ctx context.Context) error {
	return c.postBotsonAPI(ctx, "/botson/api/discord/stop")
}

// DiscordStatus calls GET /botson/api/discord/status and reports whether
// the gateway is currently running.
func (c *Client) DiscordStatus(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/botson/api/discord/status", nil)
	if err != nil {
		return false, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to reach core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("core returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}

	var out struct {
		Running bool `json:"running"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("failed to decode discord status response: %w", err)
	}
	return out.Running, nil
}

func (c *Client) postBotsonAPI(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("core returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}
