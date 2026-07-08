package tools

import (
	"fmt"

	"botsonv2/core/interface/discord"

	"google.golang.org/adk/v2/agent"
)

// ToggleDiscordArgs defines the input arguments for the Toggle Discord tool.
type ToggleDiscordArgs struct {
	Action string `json:"action" jsonschema:"Either 'start' or 'stop'."`
}

// ToggleDiscordResult reports the gateway's state after the toggle.
type ToggleDiscordResult struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
}

// ToggleDiscord lets the agent start or stop the Discord gateway running
// within the same core process this tool call itself executes in -- a
// plain in-process function call (core/interface/discord's singleton),
// not a new OS process, so it's fast and the agent can do it for the
// user without leaving the conversation. Calls core/interface/discord
// directly rather than going through core/management, since
// core/management already depends on core/agent, which depends on this
// package (core/tools) -- routing through it here would create an import
// cycle. See AGENTS.md's "Conventions" section for the import-direction
// rule this follows.
func ToggleDiscord(ctx agent.Context, args ToggleDiscordArgs) (ToggleDiscordResult, error) {
	switch args.Action {
	case "start":
		if err := discord.StartGateway(); err != nil {
			return ToggleDiscordResult{}, err
		}
		return ToggleDiscordResult{Running: true, Message: "Discord gateway started"}, nil
	case "stop":
		if err := discord.StopGateway(); err != nil {
			return ToggleDiscordResult{}, err
		}
		return ToggleDiscordResult{Running: false, Message: "Discord gateway stopped"}, nil
	default:
		return ToggleDiscordResult{}, fmt.Errorf("action must be 'start' or 'stop' (got %q)", args.Action)
	}
}
