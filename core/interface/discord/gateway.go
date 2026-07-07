package discord

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"botsonv2/core/config"
	"google.golang.org/adk/v2/cmd/launcher"
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
