package discord

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"
)

func (g *Gateway) sendDiscordConfirmationRequest(s *discordgo.Session, channelID, sessionID string, fc *genai.FunctionCall) {
	hint := "An agent tool execution requires confirmation."
	if h, ok := fc.Args["hint"].(string); ok {
		hint = h
	}

	approveID := fmt.Sprintf("hitl:approve:%s:%s", sessionID, fc.ID)
	denyID := fmt.Sprintf("hitl:deny:%s:%s", sessionID, fc.ID)

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       "⚠️ Agent Confirmation Required",
				Description: hint,
				Color:       0xF59E0B, // Amber
				Fields: []*discordgo.MessageEmbedField{
					{Name: "Function Name", Value: fmt.Sprintf("`%s`", fc.Name), Inline: true},
					{Name: "Request ID", Value: fmt.Sprintf("`%s`", fc.ID), Inline: true},
				},
			},
		},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Approve",
						Style:    discordgo.SuccessButton,
						CustomID: approveID,
					},
					discordgo.Button{
						Label:    "Deny",
						Style:    discordgo.DangerButton,
						CustomID: denyID,
					},
				},
			},
		},
	}

	_, err := s.ChannelMessageSendComplex(channelID, msg)
	if err != nil {
		log.Printf("Failed to send Discord confirmation buttons: %v", err)
	}
}

func (g *Gateway) handleMessageComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "hitl:") {
		return
	}

	userID := ""
	var member *discordgo.Member
	if i.Member != nil {
		member = i.Member
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if !g.isAuthorized(userID, member) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ You are not authorized to approve tool calls for this bot.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	parts := strings.Split(customID, ":")
	if len(parts) < 4 {
		return
	}

	action := parts[1] // "approve" or "deny"
	sessionID := parts[2]
	callID := parts[3]

	confirmed := action == "approve"

	var responseText string
	var color int
	username := userID
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
	} else if i.User != nil {
		username = i.User.Username
	}

	if confirmed {
		responseText = "✅ Tool execution was APPROVED by @" + username
		color = 0x10B981
	} else {
		responseText = "❌ Tool execution was DENIED by @" + username
		color = 0xEF4444
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "⚡ Agent Confirmation Resolved",
					Description: responseText,
					Color:       color,
				},
			},
			Components: []discordgo.MessageComponent{},
		},
	})

	go g.resumeAgentRunner(s, i.ChannelID, sessionID, callID, confirmed)
}

func (g *Gateway) resumeAgentRunner(s *discordgo.Session, channelID, sessionID, callID string, confirmed bool) {
	ctx := context.Background()
	rootAgent := g.config.AgentLoader.RootAgent()
	if rootAgent == nil {
		s.ChannelMessageSend(channelID, "⚠️ Error: Root agent not loaded.")
		return
	}

	r, err := runner.New(runner.Config{
		AppName:           rootAgent.Name(),
		Agent:             rootAgent,
		SessionService:    g.config.SessionService,
		ArtifactService:   g.config.ArtifactService,
		AutoCreateSession: true,
	})
	if err != nil {
		s.ChannelMessageSend(channelID, "⚠️ Failed to initialize ADK runner.")
		return
	}

	s.ChannelTyping(channelID)

	resMsg := genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{
				FunctionResponse: &genai.FunctionResponse{
					Name: "adk_request_confirmation",
					ID:   callID,
					Response: map[string]any{
						"confirmed": confirmed,
					},
				},
			},
		},
	}

	runIter := r.Run(ctx, "discord", sessionID, &resMsg, agent.RunConfig{})

	var responseBuilder strings.Builder
	for event, err := range runIter {
		if err != nil {
			log.Printf("Execution runner error: %v", err)
			s.ChannelMessageSend(channelID, fmt.Sprintf("❌ Execution failed: %v", err))
			return
		}

		if event == nil {
			continue
		}

		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					responseBuilder.WriteString(part.Text)
				}
			}
		}

		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil {
					if part.FunctionCall.Name == "adk_request_confirmation" {
						g.sendDiscordConfirmationRequest(s, channelID, sessionID, part.FunctionCall)
					} else {
						s.ChannelMessageSend(channelID, fmt.Sprintf("⚙️ *Executing tool `%s`...*", part.FunctionCall.Name))
					}
				}
			}
		}
	}

	finalReply := responseBuilder.String()
	if finalReply != "" {
		for len(finalReply) > 2000 {
			s.ChannelMessageSend(channelID, finalReply[:2000])
			finalReply = finalReply[2000:]
		}
		s.ChannelMessageSend(channelID, finalReply)
	}
}
