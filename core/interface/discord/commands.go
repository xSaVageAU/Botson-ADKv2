package discord

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"botsonv2/core/config"
	"google.golang.org/adk/v2/session"
)

func (g *Gateway) executeApproveCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cfg, err := config.Load()
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "❌ **Failed to load configurations**.",
			},
		})
		return
	}

	var executingUserID string
	isAdmin := false
	if i.Member != nil {
		executingUserID = i.Member.User.ID
		isAdmin = i.Member.Permissions&discordgo.PermissionAdministrator != 0
	} else if i.User != nil {
		executingUserID = i.User.ID
	}

	isOwner := cfg.Discord.OwnerID != "" && executingUserID == cfg.Discord.OwnerID
	if !isOwner && !isAdmin {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "❌ **Permission Denied**: Only the bot owner or server administrators can approve access codes.",
			},
		})
		return
	}

	options := i.ApplicationCommandData().Options
	var codeVal string
	if len(options) > 0 {
		codeVal = options[0].StringValue()
	}

	if codeVal == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: "❌ **Failed**: Access code parameter is missing.",
			},
		})
		return
	}

	approvedUserID, err := ApproveRequest(codeVal)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: fmt.Sprintf("❌ **Approval Failed**: %v", err),
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "✅ Access Granted",
					Description: fmt.Sprintf("User <@%s> has been successfully whitelisted!", approvedUserID),
					Color:       0x33ff33,
				},
			},
		},
	})
}

func (g *Gateway) executeNewCommand(s *discordgo.Session, i *discordgo.InteractionCreate, ctx context.Context, agentName string) {
	channelID := i.ChannelID
	var user *discordgo.User
	if i.Member != nil {
		user = i.Member.User
	} else {
		user = i.User
	}
	channelName := g.getChannelName(s, channelID, user)

	g.mu.RLock()
	oldSessionID := g.activeSessions[channelID]
	g.mu.RUnlock()

	newSessionID := uuid.New().String()
	_, err := g.config.SessionService.Create(ctx, &session.CreateRequest{
		AppName:   agentName,
		UserID:    "discord",
		SessionID: newSessionID,
		State: map[string]any{
			"__session_metadata__": map[string]any{
				"displayName":        fmt.Sprintf("Discord - #%s", channelName),
				"discord_channel_id": channelID,
			},
		},
	})
	if err != nil {
		g.followUpError(s, i, "Failed to create session: "+err.Error())
		return
	}

	g.mu.Lock()
	g.activeSessions[channelID] = newSessionID
	g.mu.Unlock()
	_ = g.saveActiveSessions()

	oldText := oldSessionID
	if oldText == "" {
		oldText = "None"
	}

	embed := &discordgo.MessageEmbed{
		Title: "🔄 Session Reset",
		Color: 0x3882F6,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Old Session ID", Value: fmt.Sprintf("`%s`", oldText), Inline: true},
			{Name: "New Session ID", Value: fmt.Sprintf("`%s`", newSessionID), Inline: true},
			{Name: "Active Agent", Value: fmt.Sprintf("`%s`", agentName), Inline: false},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

func (g *Gateway) executeListCommand(s *discordgo.Session, i *discordgo.InteractionCreate, ctx context.Context, agentName string) {
	channelID := i.ChannelID
	sessions, err := g.getChannelSessions(ctx, channelID, agentName)
	if err != nil {
		g.followUpError(s, i, "Failed to fetch session list: "+err.Error())
		return
	}

	if len(sessions) == 0 {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "📋 No sessions recorded for this channel yet. Send a message or use `/new` to create one.",
		})
		return
	}

	var sb strings.Builder
	for idx, sess := range sessions {
		if idx >= 10 {
			break
		}
		title := sess.ID()
		val, err := sess.State().Get("__session_metadata__")
		if err == nil {
			if metaMap, ok := val.(map[string]any); ok {
				if dn, ok := metaMap["displayName"].(string); ok {
					title = dn
				}
			}
		}
		timeStr := sess.LastUpdateTime().Format("2006-01-02 15:04:05")
		sb.WriteString(fmt.Sprintf("%d. `%s` - %s (%s)\n", idx+1, sess.ID(), title, timeStr))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "📋 Channel Session Registry",
		Color:       0x3882F6,
		Description: sb.String(),
		Timestamp:   time.Now().Format(time.RFC3339),
	}
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

func (g *Gateway) executeSelectCommand(s *discordgo.Session, i *discordgo.InteractionCreate, ctx context.Context, agentName string) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		g.followUpError(s, i, "Missing required argument 'index'.")
		return
	}
	idxValue := options[0].IntValue()
	if idxValue <= 0 {
		g.followUpError(s, i, "Index must be greater than 0.")
		return
	}

	channelID := i.ChannelID
	sessions, err := g.getChannelSessions(ctx, channelID, agentName)
	if err != nil {
		g.followUpError(s, i, "Failed to load channel sessions: "+err.Error())
		return
	}

	idx := int(idxValue) - 1
	if idx >= len(sessions) {
		g.followUpError(s, i, fmt.Sprintf("Invalid index %d. Only %d sessions found.", idxValue, len(sessions)))
		return
	}

	selectedSession := sessions[idx]

	g.mu.Lock()
	oldSessionID := g.activeSessions[channelID]
	g.activeSessions[channelID] = selectedSession.ID()
	g.mu.Unlock()
	_ = g.saveActiveSessions()

	oldText := oldSessionID
	if oldText == "" {
		oldText = "None"
	}

	embed := &discordgo.MessageEmbed{
		Title: "🔄 Session Activated",
		Color: 0x10B981,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Old Session ID", Value: fmt.Sprintf("`%s`", oldText), Inline: true},
			{Name: "Active Session ID", Value: fmt.Sprintf("`%s`", selectedSession.ID()), Inline: true},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

func (g *Gateway) executeInfoCommand(s *discordgo.Session, i *discordgo.InteractionCreate, ctx context.Context, agentName string) {
	channelID := i.ChannelID
	activeSessionID, err := g.resolveSessionID(ctx, channelID, agentName)
	if err != nil {
		g.followUpError(s, i, "Failed to resolve active session: "+err.Error())
		return
	}

	resp, err := g.config.SessionService.Get(ctx, &session.GetRequest{
		AppName:   agentName,
		UserID:    "discord",
		SessionID: activeSessionID,
	})
	if err != nil {
		g.followUpError(s, i, "Session details not found in database: "+err.Error())
		return
	}

	sess := resp.Session
	msgCount := sess.Events().Len()
	lastUpdate := sess.LastUpdateTime().Format("2006-01-02 15:04:05")

	embed := &discordgo.MessageEmbed{
		Title: "ℹ️ Active Session Context",
		Color: 0x3882F6,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Session ID", Value: fmt.Sprintf("`%s`", sess.ID()), Inline: false},
			{Name: "Target Agent", Value: fmt.Sprintf("`%s`", agentName), Inline: true},
			{Name: "Total Events", Value: strconv.Itoa(msgCount), Inline: true},
			{Name: "Last Update", Value: lastUpdate, Inline: false},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}
