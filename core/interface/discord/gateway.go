package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"botsonv2/core/config"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type Gateway struct {
	session        *discordgo.Session
	config         *launcher.Config
	activeSessions map[string]string // maps ChannelID -> SessionID (UUID)
	mu             sync.RWMutex
}

func New(token string, config *launcher.Config) (*Gateway, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	g := &Gateway{
		session:        dg,
		config:         config,
		activeSessions: make(map[string]string),
	}
	g.loadActiveSessions()
	return g, nil
}

func (g *Gateway) saveActiveSessions() error {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return err
	}
	filePath := filepath.Join(dataDir, "discord_active_sessions.json")

	g.mu.RLock()
	data, err := json.MarshalIndent(g.activeSessions, "", "  ")
	g.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func (g *Gateway) loadActiveSessions() {
	dataDir, err := config.GetDataDir()
	if err != nil {
		log.Printf("Discord Warning: failed to resolve data directory: %v", err)
		return
	}
	filePath := filepath.Join(dataDir, "discord_active_sessions.json")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Discord Warning: failed to read active sessions file: %v", err)
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if err := json.Unmarshal(data, &g.activeSessions); err != nil {
		log.Printf("Discord Warning: failed to parse active sessions JSON: %v", err)
	} else {
		log.Printf("Discord Info: successfully loaded %d active sessions from disk.", len(g.activeSessions))
	}
}

func (g *Gateway) Start() error {
	g.session.AddHandler(g.handleInteraction)
	g.session.AddHandler(g.handleMessage)
	g.session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages

	if err := g.session.Open(); err != nil {
		return err
	}

	// Register Slash Commands
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "new",
			Description: "Start a fresh chat session in this channel",
		},
		{
			Name:        "list",
			Description: "List recent chat sessions in this channel",
		},
		{
			Name:        "select",
			Description: "Select an active session by index for this channel",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "index",
					Description: "The session index number from the /list command",
					Required:    true,
				},
			},
		},
		{
			Name:        "info",
			Description: "Show details of the currently active session in this channel",
		},
		{
			Name:        "approve",
			Description: "Approve a user's pending access code (Admin/Owner only)",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "code",
					Description: "The authorization code to approve (e.g. AUTH-123456)",
					Required:    true,
				},
			},
		},
	}

	for _, cmd := range commands {
		_, err := g.session.ApplicationCommandCreate(g.session.State.User.ID, "", cmd)
		if err != nil {
			log.Printf("Cannot create application command %q: %v", cmd.Name, err)
		}
	}

	// Send Boot DM to owner if configured
	cfg, err := config.Load()
	if err == nil && cfg.Discord.OwnerID != "" {
		hostOS := runtime.GOOS
		rootAgentName := "None"
		if rootAgent := g.config.AgentLoader.RootAgent(); rootAgent != nil {
			rootAgentName = rootAgent.Name()
		}

		activeSessionID := "None"
		dm, dmErr := g.session.UserChannelCreate(cfg.Discord.OwnerID)
		if dmErr == nil {
			g.mu.RLock()
			if sessID, ok := g.activeSessions[dm.ID]; ok && sessID != "" {
				activeSessionID = sessID
			}
			g.mu.RUnlock()
		}

		embed := &discordgo.MessageEmbed{
			Title:       "🟢 Botson Gateway Online",
			Color:       0x10B981,
			Description: "Your Botson Workspace Console gateway is active and listening for messages.",
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Host System", Value: fmt.Sprintf("`%s`", hostOS), Inline: true},
				{Name: "Default Agent", Value: fmt.Sprintf("`%s`", rootAgentName), Inline: true},
				{Name: "Active Session ID", Value: fmt.Sprintf("`%s`", activeSessionID), Inline: false},
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
		g.sendOwnerNotification(embed)
	}

	return nil
}

func (g *Gateway) Close() error {
	// Send Shutdown DM to owner if configured
	cfg, err := config.Load()
	if err == nil && cfg.Discord.OwnerID != "" {
		activeSessionID := "None"
		dm, dmErr := g.session.UserChannelCreate(cfg.Discord.OwnerID)
		if dmErr == nil {
			g.mu.RLock()
			if sessID, ok := g.activeSessions[dm.ID]; ok && sessID != "" {
				activeSessionID = sessID
			}
			g.mu.RUnlock()
		}

		embed := &discordgo.MessageEmbed{
			Title:       "🔴 Botson Gateway Offline",
			Color:       0xEF4444,
			Description: "Your Botson Workspace Console gateway is shutting down gracefully.",
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Exit Status", Value: "`Clean Shutdown`", Inline: true},
				{Name: "Active Session ID", Value: fmt.Sprintf("`%s`", activeSessionID), Inline: true},
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
		g.sendOwnerNotification(embed)
	}

	return g.session.Close()
}

func (g *Gateway) sendOwnerNotification(embed *discordgo.MessageEmbed) {
	cfg, err := config.Load()
	if err != nil || cfg.Discord.OwnerID == "" {
		return
	}

	dm, err := g.session.UserChannelCreate(cfg.Discord.OwnerID)
	if err != nil {
		log.Printf("Discord Warning: failed to create DM channel with owner: %v", err)
		return
	}

	_, err = g.session.ChannelMessageSendEmbed(dm.ID, embed)
	if err != nil {
		log.Printf("Discord Warning: failed to send DM embed to owner: %v", err)
	}
}

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
		code := GetManager().AddPendingRequest(userID, user.Username, i.ChannelID)
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

	approvedUserID, err := GetManager().ApproveRequest(codeVal)
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
			code := GetManager().AddPendingRequest(m.Author.ID, m.Author.Username, m.ChannelID)
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

// Helper: Resolve the active Session ID for the channel
func (g *Gateway) resolveSessionID(ctx context.Context, channelID string, agentName string) (string, error) {
	g.mu.RLock()
	sessionID, ok := g.activeSessions[channelID]
	g.mu.RUnlock()
	if ok && sessionID != "" {
		return sessionID, nil
	}

	// Look up in GORM DB for matching channel metadata
	sessions, err := g.getChannelSessions(ctx, channelID, agentName)
	if err == nil && len(sessions) > 0 {
		sessionID = sessions[0].ID()
		g.mu.Lock()
		g.activeSessions[channelID] = sessionID
		g.mu.Unlock()
		_ = g.saveActiveSessions()
		return sessionID, nil
	}

	// Create new session UUID if not found
	sessionID = uuid.New().String()
	_, err = g.config.SessionService.Create(ctx, &session.CreateRequest{
		AppName:   agentName,
		UserID:    "discord",
		SessionID: sessionID,
		State: map[string]any{
			"__session_metadata__": map[string]any{
				"displayName":        fmt.Sprintf("Discord - #%s", channelID),
				"discord_channel_id": channelID,
			},
		},
	})
	if err != nil {
		return "", err
	}

	g.mu.Lock()
	g.activeSessions[channelID] = sessionID
	g.mu.Unlock()
	_ = g.saveActiveSessions()

	return sessionID, nil
}

func (g *Gateway) getChannelSessions(ctx context.Context, channelID, agentName string) ([]session.Session, error) {
	listResp, err := g.config.SessionService.List(ctx, &session.ListRequest{
		AppName: agentName,
		UserID:  "discord",
	})
	if err != nil {
		return nil, err
	}

	var results []session.Session
	for _, sess := range listResp.Sessions {
		val, err := sess.State().Get("__session_metadata__")
		if err != nil {
			continue
		}
		var metaMap map[string]any
		
		// Handle potential database driver parsing variants
		switch v := val.(type) {
		case map[string]any:
			metaMap = v
		case string:
			json.Unmarshal([]byte(v), &metaMap)
		}

		if metaMap != nil {
			if chanID, ok := metaMap["discord_channel_id"].(string); ok && chanID == channelID {
				results = append(results, sess)
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].LastUpdateTime().After(results[j].LastUpdateTime())
	})

	return results, nil
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
