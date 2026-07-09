package natscore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
)

// Serve subscribes to every subject in this package and answers requests
// using cfg's agent loader/session service/artifact service directly, then
// blocks until ctx is done, at which point it unsubscribes and returns.
func Serve(ctx context.Context, nc *nats.Conn, cfg *launcher.Config) error {
	subjects := []struct {
		subject string
		handler nats.MsgHandler
	}{
		{SubjectDefaultAgent, handleDefaultAgent(cfg)},
		{SubjectSessionCreate, handleSessionCreate(ctx, cfg)},
		{SubjectSessionGet, handleSessionGet(ctx, cfg)},
		{SubjectRun, handleRun(ctx, nc, cfg)},
	}

	var subs []*nats.Subscription
	for _, s := range subjects {
		sub, err := nc.Subscribe(s.subject, s.handler)
		if err != nil {
			for _, existing := range subs {
				_ = existing.Unsubscribe()
			}
			return fmt.Errorf("failed to subscribe to %s: %w", s.subject, err)
		}
		subs = append(subs, sub)
	}

	<-ctx.Done()
	for _, sub := range subs {
		_ = sub.Unsubscribe()
	}
	return nil
}

// respond marshals v and replies to msg, discarding a marshal error since
// there is nothing more useful to do with it than not answer at all --
// v's fields are always trivially JSON-encodable wire types.
func respond(msg *nats.Msg, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = msg.Respond(data)
}

// respondError replies with a bare {"error": "..."} object, which every
// reply type in this package can decode (each embeds an Error field with
// the same JSON tag) regardless of which subject it answers.
func respondError(msg *nats.Msg, err error) {
	respond(msg, map[string]string{"error": err.Error()})
}

func handleDefaultAgent(cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		root := cfg.AgentLoader.RootAgent()
		respond(msg, DefaultAgentReply{Name: root.Name()})
	}
}

func handleSessionCreate(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req SessionCreateRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		resp, err := cfg.SessionService.Create(ctx, &session.CreateRequest{
			AppName:   req.AppName,
			UserID:    req.UserID,
			SessionID: req.SessionID,
			State:     req.State,
		})
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, SessionCreateReply{ID: resp.Session.ID()})
	}
}

func handleSessionGet(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req SessionGetRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		resp, err := cfg.SessionService.Get(ctx, &session.GetRequest{
			AppName:   req.AppName,
			UserID:    req.UserID,
			SessionID: req.SessionID,
		})
		if err != nil {
			respondError(msg, err)
			return
		}

		state := map[string]any{}
		for key, val := range resp.Session.State().All() {
			state[key] = val
		}
		var events []Event
		for evt := range resp.Session.Events().All() {
			events = append(events, Event{Author: evt.Author, Content: evt.Content})
		}

		respond(msg, SessionGetReply{SessionInfo: SessionInfo{Events: events, State: state}})
	}
}

// handleRun answers SubjectRun by publishing one Frame per session.Event to
// msg.Reply -- the same runner.Runner.Run call internal/interface/discord's
// handlers.go used to make directly against Discord, just publishing to
// NATS instead of posting to a channel. Always finishes with exactly one
// terminal Frame (Done or Error), since NATS has no implicit "stream ended"
// signal the way an HTTP response body does.
func handleRun(ctx context.Context, nc *nats.Conn, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		if msg.Reply == "" {
			return
		}

		var req RunRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			publishFrame(nc, msg.Reply, Frame{Error: err.Error()})
			return
		}

		curAgent, err := cfg.AgentLoader.LoadAgent(req.AppName)
		if err != nil {
			publishFrame(nc, msg.Reply, Frame{Error: fmt.Sprintf("failed to load agent: %s", err)})
			return
		}

		r, err := runner.New(runner.Config{
			AppName:           req.AppName,
			Agent:             curAgent,
			SessionService:    cfg.SessionService,
			ArtifactService:   cfg.ArtifactService,
			MemoryService:     cfg.MemoryService,
			PluginConfig:      cfg.PluginConfig,
			AutoCreateSession: true,
		})
		if err != nil {
			publishFrame(nc, msg.Reply, Frame{Error: fmt.Sprintf("failed to create runner: %s", err)})
			return
		}

		runCfg := agent.RunConfig{StreamingMode: agent.StreamingModeSSE}
		for ev, err := range r.Run(ctx, req.UserID, req.SessionID, req.NewMessage, runCfg) {
			if err != nil {
				publishFrame(nc, msg.Reply, Frame{Error: err.Error()})
				return
			}
			publishFrame(nc, msg.Reply, Frame{Event: &Event{Author: ev.Author, Content: ev.Content}})
		}
		publishFrame(nc, msg.Reply, Frame{Done: true})
	}
}

func publishFrame(nc *nats.Conn, replyInbox string, frame Frame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	_ = nc.Publish(replyInbox, data)
}
