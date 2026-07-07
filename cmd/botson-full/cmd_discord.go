package main

import (
	"context"
	"fmt"
	"log"

	"botsonv2/core/interface/discord"

	"github.com/spf13/cobra"
)

func newDiscordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discord",
		Short: "Start the standalone Discord gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscord(cmd.Context())
		},
	}
}

func runDiscord(ctx context.Context) error {
	token := boot.Config.Discord.Token
	if token == "" {
		return fmt.Errorf("discord.token is not defined in ~/.botsonv2/config.json")
	}

	gateway, err := discord.New(token, boot.Launcher)
	if err != nil {
		return fmt.Errorf("failed to initialize Discord gateway: %w", err)
	}

	log.Println("Starting Discord Gateway...")
	if err := gateway.Start(); err != nil {
		return fmt.Errorf("failed to start Discord gateway: %w", err)
	}
	log.Println("Discord Gateway is online. Press Ctrl+C to terminate.")

	<-ctx.Done()

	log.Println("Shutting down Discord Gateway gracefully...")
	if err := gateway.Close(); err != nil {
		log.Printf("Error closing gateway connection: %v", err)
	}
	log.Println("Discord Gateway offline. Good bye!")
	return nil
}
