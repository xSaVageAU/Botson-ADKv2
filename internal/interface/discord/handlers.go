package discord

import (
	"context"
	"fmt"
	"log"
	"strings"

	"botsonv2/internal/config"
	"github.com/bwmarrin/discordgo"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"
)

func (g *Gateway) isAuthorized(userID string, member *discordgo.Member) bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	// Owner is always authorized
	if cfg.Discord.OwnerID != "" && userID == cfg.Discord.OwnerID {
		return true
	}
	// Check whitelist
	for _, id := range cfg.Discord.Whitelist {
		if id == userID {
			return true
		}
	}
	// Fallback: If OwnerID is not configured, allow users with Administrator permissions in the server
	if cfg.Discord.OwnerID == "" && member != nil {
		if member.Permissions&discordgo.PermissionAdministrator != 0 {
			return true
		}
	}
	return false
}

func (g *Gateway) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionMessageComponent {
		g.handleMessageComponent(s, i)
		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	cmdName := i.ApplicationCommandData().Name

	var user *discordgo.User
	if i.Member != nil {
		user = i.Member.User
	} else {
		user = i.User
	}

	if user == nil {
		return
	}
	userID := user.ID

	if cmdName == "approve" {
		g.executeApproveCommand(s, i)
		return
	}

	// Check whitelisting authorization gate
	if !g.isAuthorized(userID, i.Member) {
		code, err := AddPendingRequest(userID, user.Username, i.ChannelID)
		if err != nil {
			log.Printf("Failed to register pending authorization request: %v", err)
			code = "unavailable, please try again"
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags: discordgo.MessageFlagsEphemeral,
				Embeds: []*discordgo.MessageEmbed{
					{
						Title:       "🔒 Access Denied",
						Description: fmt.Sprintf("You are not authorized to interact with this agent.\n\nPlease ask the bot administrator to approve your access code:\n\n**`%s`**", code),
						Color:       0xff3333,
					},
				},
			},
		})
		return
	}

	rootAgent := g.config.AgentLoader.RootAgent()
	if rootAgent == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ Error: No root agent loaded in this workspace.",
			},
		})
		return
	}

	// Defer response to allow GORM DB queries to finish safely
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	ctx := context.Background()

	switch cmdName {
	case "new":
		g.executeNewCommand(s, i, ctx, rootAgent.Name())
	case "list":
		g.executeListCommand(s, i, ctx, rootAgent.Name())
	case "select":
		g.executeSelectCommand(s, i, ctx, rootAgent.Name())
	case "info":
		g.executeInfoCommand(s, i, ctx, rootAgent.Name())
	}
}

func (g *Gateway) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Author.Bot {
		return
	}

	content := strings.TrimSpace(m.Content)
	if content == "" {
		return
	}

	// Direct Messages (always respond) or Mentions in Channels
	isDM := m.GuildID == ""
	mentioned := false
	for _, u := range m.Mentions {
		if u.ID == s.State.User.ID {
			mentioned = true
			break
		}
	}

	if isDM || mentioned {
		// Whitelist Authorization check
		if !g.isAuthorized(m.Author.ID, m.Member) {
			code, err := AddPendingRequest(m.Author.ID, m.Author.Username, m.ChannelID)
			if err != nil {
				log.Printf("Failed to register pending authorization request: %v", err)
				code = "unavailable, please try again"
			}
			embed := &discordgo.MessageEmbed{
				Title:       "🔒 Access Denied",
				Description: fmt.Sprintf("You are not authorized to interact with this agent.\n\nPlease ask the bot administrator to approve your access code:\n\n**`%s`**", code),
				Color:       0xff3333,
			}
			s.ChannelMessageSendEmbed(m.ChannelID, embed)
			return
		}

		text := content
		// Strip mentions
		text = strings.ReplaceAll(text, fmt.Sprintf("<@%s>", s.State.User.ID), "")
		text = strings.ReplaceAll(text, fmt.Sprintf("<@!%s>", s.State.User.ID), "")
		text = strings.TrimSpace(text)
		if text != "" {
			g.handleConversation(s, m, text)
		}
	}
}

func (g *Gateway) handleConversation(s *discordgo.Session, m *discordgo.MessageCreate, text string) {
	ctx := context.Background()
	rootAgent := g.config.AgentLoader.RootAgent()
	if rootAgent == nil {
		s.ChannelMessageSend(m.ChannelID, "⚠️ Error: No root agent loaded in this workspace.")
		return
	}

	activeSessionID, err := g.resolveSessionID(ctx, m.ChannelID, rootAgent.Name())
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "⚠️ Error: Failed to initialize chat session context: "+err.Error())
		return
	}

	// Create Runner
	r, err := runner.New(runner.Config{
		AppName:           rootAgent.Name(),
		Agent:             rootAgent,
		SessionService:    g.config.SessionService,
		ArtifactService:   g.config.ArtifactService,
		AutoCreateSession: true,
	})
	if err != nil {
		log.Printf("Failed to create runner: %v", err)
		s.ChannelMessageSend(m.ChannelID, "⚠️ Failed to initialize ADK runner.")
		return
	}

	// Signal typing
	s.ChannelTyping(m.ChannelID)

	// Build user input message
	userMsg := genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: text}},
	}

	runIter := r.Run(ctx, "discord", activeSessionID, &userMsg, agent.RunConfig{})

	var responseBuilder strings.Builder
	for event, err := range runIter {
		if err != nil {
			log.Printf("Execution runner error: %v", err)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("❌ Execution failed: %v", err))
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
						g.sendDiscordConfirmationRequest(s, m.ChannelID, activeSessionID, part.FunctionCall)
					} else {
						s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("⚙️ *Executing tool `%s`...*", part.FunctionCall.Name))
					}
				}
			}
		}
	}

	finalReply := responseBuilder.String()
	if finalReply != "" {
		for len(finalReply) > 2000 {
			s.ChannelMessageSend(m.ChannelID, finalReply[:2000])
			finalReply = finalReply[2000:]
		}
		s.ChannelMessageSend(m.ChannelID, finalReply)
	}
}

func (g *Gateway) getChannelName(s *discordgo.Session, channelID string, user *discordgo.User) string {
	if s != nil {
		channel, err := s.Channel(channelID)
		if err == nil && channel != nil && channel.Name != "" {
			return channel.Name
		}
	}
	if user != nil {
		return "DM with @" + user.Username
	}
	return channelID
}

func (g *Gateway) followUpError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: "⚠️ " + message,
	})
}
