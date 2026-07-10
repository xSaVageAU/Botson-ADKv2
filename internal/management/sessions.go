package management

import (
	"context"
	"fmt"
	"sort"
	"time"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// AutoModeStateKey is the flat session-state key that turns "auto mode" on
// for one session -- a durable, per-session flag internal/automode's
// background worker polls for, and Botson-TUI reads back from
// SessionsGet's State map to know whether to auto-answer confirmations
// itself while connected. Flat (not nested in a map), same convention as
// "botson:cwd" and "botson:tools:read:<path>" -- see
// internal/tools/read_tracking.go for why flat keys survive the JSON
// round-trip on reload where a nested map value wouldn't.
const AutoModeStateKey = "botson:autoMode"

// SessionEventSummary is one event in a session's history, rendered down
// to what's useful to display -- who said it, when, and its text (tool
// calls are rendered as a bracketed marker rather than raw JSON args).
type SessionEventSummary struct {
	Author    string    `json:"author"`
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
}

// SessionDetail is a single session's full detail: its summary stats plus
// state and event history.
type SessionDetail struct {
	SessionStat
	State  map[string]any        `json:"state"`
	Events []SessionEventSummary `json:"events"`
}

// ListSessions returns a SessionStat for every session under agentNames
// (as returned by ListAgents), most-recently-updated first, optionally
// narrowed to a single agent and/or user. Unlike GetDashboardStats, this
// only needs a session.Service -- no live Gemini model or agent loader --
// so it works even without a configured API key, same as ListAgents.
func ListSessions(ctx context.Context, svc session.Service, agentNames []string, filterAgent, filterUser string) ([]SessionStat, error) {
	var stats []SessionStat

	for _, name := range agentNames {
		if filterAgent != "" && name != filterAgent {
			continue
		}

		resp, err := svc.List(ctx, &session.ListRequest{AppName: name, UserID: filterUser})
		if err != nil || resp == nil {
			continue // an agent with no sessions yet isn't an error
		}

		for _, s := range resp.Sessions {
			stats = append(stats, toSessionStat(s))
		}
	}

	sort.Slice(stats, func(i, j int) bool { return stats[i].LastUpdateTime > stats[j].LastUpdateTime })
	return stats, nil
}

// GetSession returns full detail (state + event history) for one session,
// identified by its full composite key.
func GetSession(ctx context.Context, svc session.Service, appName, userID, sessionID string) (*SessionDetail, error) {
	resp, err := svc.Get(ctx, &session.GetRequest{AppName: appName, UserID: userID, SessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	s := resp.Session
	detail := &SessionDetail{
		SessionStat: toSessionStat(s),
		State:       map[string]any{},
	}

	for key, val := range s.State().All() {
		detail.State[key] = val
	}

	for evt := range s.Events().All() {
		text := ""
		if evt.Content != nil {
			for _, part := range evt.Content.Parts {
				switch {
				case part.Text != "":
					text += part.Text
				case part.FunctionCall != nil:
					text += fmt.Sprintf("[tool call: %s]", part.FunctionCall.Name)
				case part.FunctionResponse != nil:
					text += fmt.Sprintf("[tool response: %s]", part.FunctionResponse.Name)
				}
			}
		}
		detail.Events = append(detail.Events, SessionEventSummary{
			Author:    evt.Author,
			Timestamp: evt.Timestamp,
			Text:      text,
		})
	}

	return detail, nil
}

// SetSessionAutoMode durably sets or clears a session's own AutoModeStateKey
// flag by appending a synthetic event carrying just a StateDelta -- the same
// persistence path a real turn's own state changes already go through
// (session.Service.AppendEvent), so the change survives a core restart and
// is visible to internal/automode's polling loop immediately, without
// waiting on any client to send a turn. reason, if non-empty, is also
// recorded as a plain text part on the same event (e.g. when automode
// itself disables the flag after hitting its safety cap) so a returning
// user sees why without checking logs.
func SetSessionAutoMode(ctx context.Context, svc session.Service, appName, userID, sessionID string, enabled bool, reason string) error {
	resp, err := svc.Get(ctx, &session.GetRequest{AppName: appName, UserID: userID, SessionID: sessionID})
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	ev := session.NewEvent(ctx, "botson-automode-toggle")
	ev.Author = "system"
	ev.Actions.StateDelta[AutoModeStateKey] = enabled
	if reason != "" {
		ev.Content = &genai.Content{Role: "model", Parts: []*genai.Part{{Text: reason}}}
	}

	if err := svc.AppendEvent(ctx, resp.Session, ev); err != nil {
		return fmt.Errorf("failed to persist auto-mode flag: %w", err)
	}
	return nil
}

// DeleteSession removes a session by its full composite key. Matches the
// underlying database service's own semantics: deleting a session that
// doesn't exist is not an error.
func DeleteSession(ctx context.Context, svc session.Service, appName, userID, sessionID string) error {
	if err := svc.Delete(ctx, &session.DeleteRequest{AppName: appName, UserID: userID, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}
