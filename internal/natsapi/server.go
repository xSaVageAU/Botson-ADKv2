package natsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"google.golang.org/adk/v2/cmd/launcher"

	"botsonv2/internal/agent"
	"botsonv2/internal/config"
	"botsonv2/internal/management"
	"botsonv2/internal/scripts"
)

// Serve subscribes to every subject in this package, answering requests
// using cfg's agent loader/session service directly (for the
// dashboard/sessions handlers) or the config/scripts packages directly (for
// everything else, which needs no launcher at all), then blocks until ctx
// is done, at which point it unsubscribes and returns.
func Serve(ctx context.Context, nc *nats.Conn, cfg *launcher.Config) error {
	subjects := []struct {
		subject string
		handler nats.MsgHandler
	}{
		{SubjectSettingsGet, handleSettingsGet()},
		{SubjectSettingsSet, handleSettingsSet()},

		{SubjectAgentsList, handleAgentsList()},
		{SubjectAgentsTools, handleAgentsTools()},
		{SubjectAgentsSave, handleAgentsSave()},
		{SubjectAgentsDelete, handleAgentsDelete()},

		{SubjectSessionsList, handleSessionsList(ctx, cfg)},
		{SubjectSessionsGet, handleSessionsGet(ctx, cfg)},
		{SubjectSessionsDelete, handleSessionsDelete(ctx, cfg)},

		{SubjectScriptsList, handleScriptsList()},
		{SubjectScriptsGet, handleScriptsGet()},
		{SubjectScriptsSave, handleScriptsSave()},
		{SubjectScriptsDelete, handleScriptsDelete()},
		{SubjectScriptsRun, handleScriptsRun(ctx)},

		{SubjectDashboardStats, handleDashboardStats(ctx, cfg)},
		{SubjectDashboardUsers, handleDashboardUsers(ctx, cfg)},
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
// there is nothing more useful to do with it than not answer at all -- v's
// fields are always trivially JSON-encodable wire types.
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

func handleSettingsGet() nats.MsgHandler {
	return func(msg *nats.Msg) {
		cfg, err := management.GetMaskedConfig()
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, cfg)
	}
}

func handleSettingsSet() nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req SettingsSetRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		cfg, err := config.Update(func(cfg *config.AppConfig) {
			if req.ModelName != nil {
				cfg.ModelName = *req.ModelName
			}
			if req.RootAgent != nil {
				cfg.RootAgent = *req.RootAgent
			}
			if req.GeminiAPIKey != nil {
				cfg.GeminiAPIKey = *req.GeminiAPIKey
			}
		})
		if err != nil {
			respondError(msg, err)
			return
		}

		respond(msg, config.Mask(cfg))
	}
}

func handleAgentsList() nats.MsgHandler {
	return func(msg *nats.Msg) {
		details, err := management.ListAgents()
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, details)
	}
}

func handleAgentsTools() nats.MsgHandler {
	return func(msg *nats.Msg) {
		tools, err := management.ListTools()
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, tools)
	}
}

func handleAgentsSave() nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req AgentsSaveRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		detail := agent.AgentDetail{
			AgentConfig: agent.AgentConfig{
				Name:        req.Name,
				Description: req.Description,
				Private:     req.Private,
				Tools:       req.Tools,
			},
			Instructions: req.Instructions,
		}
		if err := management.SaveAgent(detail); err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, map[string]string{})
	}
}

func handleAgentsDelete() nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req AgentsDeleteRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}
		if err := management.DeleteAgent(req.Name); err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, map[string]string{})
	}
}

// sessionsAgentNames returns every known agent name, for ListSessions to
// scan across when no --agent-equivalent filter narrows the request.
func sessionsAgentNames(cfg *launcher.Config) []string {
	if cfg == nil || cfg.AgentLoader == nil {
		return nil
	}
	return cfg.AgentLoader.ListAgents()
}

func handleSessionsList(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req SessionsListRequest
		if len(msg.Data) > 0 {
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				respondError(msg, err)
				return
			}
		}

		stats, err := management.ListSessions(ctx, cfg.SessionService, sessionsAgentNames(cfg), req.Agent, req.User)
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, stats)
	}
}

func handleSessionsGet(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req SessionsGetRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		detail, err := management.GetSession(ctx, cfg.SessionService, req.Agent, req.User, req.SessionID)
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, detail)
	}
}

func handleSessionsDelete(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req SessionsDeleteRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		if err := management.DeleteSession(ctx, cfg.SessionService, req.Agent, req.User, req.SessionID); err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, map[string]string{})
	}
}

func handleScriptsList() nats.MsgHandler {
	return func(msg *nats.Msg) {
		details, err := scripts.List()
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, details)
	}
}

func handleScriptsGet() nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req ScriptsGetRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		details, err := scripts.List()
		if err != nil {
			respondError(msg, err)
			return
		}
		for _, d := range details {
			if d.Name == req.Name {
				respond(msg, d)
				return
			}
		}
		respondError(msg, fmt.Errorf("script %q not found", req.Name))
	}
}

func handleScriptsSave() nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req ScriptsSaveRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		detail := scripts.Detail{Name: req.Name, Description: req.Description, Source: req.Source}
		if err := scripts.Save(detail); err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, map[string]string{})
	}
}

func handleScriptsDelete() nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req ScriptsDeleteRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}
		if err := scripts.Delete(req.Name); err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, map[string]string{})
	}
}

func handleScriptsRun(ctx context.Context) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var req ScriptsRunRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			respondError(msg, err)
			return
		}

		result, err := scripts.Run(ctx, req.Name, req.Args, req.TimeoutSeconds)
		if err != nil {
			respond(msg, ScriptsRunReply{Error: err.Error()})
			return
		}
		respond(msg, ScriptsRunReply{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode})
	}
}

func handleDashboardStats(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		stats, err := management.GetDashboardStats(ctx, cfg)
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, stats)
	}
}

func handleDashboardUsers(ctx context.Context, cfg *launcher.Config) nats.MsgHandler {
	return func(msg *nats.Msg) {
		users, err := management.ListSessionUsers(ctx, cfg)
		if err != nil {
			respondError(msg, err)
			return
		}
		respond(msg, users)
	}
}
