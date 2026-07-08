package main

import (
	"fmt"
	"time"

	"botsonv2/internal/config"
	"botsonv2/internal/management"
	coresession "botsonv2/internal/session"

	"github.com/spf13/cobra"
	"google.golang.org/adk/v2/session"
)

// newSessionsCmd groups listing, inspecting, and deleting chat sessions.
// Like `settings`/`agents`/`scripts`, this builds just the pieces it
// needs directly (the session DB + agent names) rather than the full
// Gemini/agent-runtime bootstrap, so it works even without a configured
// API key.
func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "sessions",
		Short:             "List, inspect, or delete chat sessions",
		PersistentPreRunE: noBootstrap,
	}
	cmd.AddCommand(newSessionsListCmd(), newSessionsShowCmd(), newSessionsDeleteCmd())
	return cmd
}

// openSessionService builds just the session database, without touching
// the Gemini model or agent loader.
func openSessionService() (session.Service, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return nil, err
	}
	return coresession.InitPersistentSessionService(dataDir)
}

func newSessionsListCmd() *cobra.Command {
	var (
		agentFilter string
		userFilter  string
		asJSON      bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List chat sessions, most recently updated first",
		RunE: func(cmd *cobra.Command, args []string) error {
			agents, err := management.ListAgents()
			if err != nil {
				return err
			}
			var agentNames []string
			for _, a := range agents {
				agentNames = append(agentNames, a.Name)
			}

			svc, err := openSessionService()
			if err != nil {
				return err
			}

			stats, err := management.ListSessions(cmd.Context(), svc, agentNames, agentFilter, userFilter)
			if err != nil {
				return err
			}

			if asJSON {
				return encodeJSON(cmd, stats)
			}
			if len(stats) == 0 {
				fmt.Println("No sessions found.")
				return nil
			}
			for i, s := range stats {
				if i > 0 {
					fmt.Println()
				}
				printSessionSummary(s)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentFilter, "agent", "", "Only show sessions for this agent")
	cmd.Flags().StringVar(&userFilter, "user", "", "Only show sessions for this user ID")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newSessionsShowCmd() *cobra.Command {
	var (
		agentName string
		userID    string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show one session's state and full event history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openSessionService()
			if err != nil {
				return err
			}

			detail, err := management.GetSession(cmd.Context(), svc, agentName, userID, args[0])
			if err != nil {
				return err
			}

			if asJSON {
				return encodeJSON(cmd, detail)
			}
			printSessionSummary(detail.SessionStat)
			fmt.Println()
			for _, evt := range detail.Events {
				fmt.Printf("[%s] %s: %s\n", evt.Timestamp.Format(time.RFC3339), evt.Author, evt.Text)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "The agent (app) name the session belongs to (required)")
	cmd.Flags().StringVar(&userID, "user", "", "The user ID the session belongs to (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("agent")
	_ = cmd.MarkFlagRequired("user")
	return cmd
}

func newSessionsDeleteCmd() *cobra.Command {
	var (
		agentName string
		userID    string
	)

	cmd := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session (use `sessions list` to find its --agent/--user)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openSessionService()
			if err != nil {
				return err
			}
			if err := management.DeleteSession(cmd.Context(), svc, agentName, userID, args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted session %q.\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "The agent (app) name the session belongs to (required)")
	cmd.Flags().StringVar(&userID, "user", "", "The user ID the session belongs to (required)")
	_ = cmd.MarkFlagRequired("agent")
	_ = cmd.MarkFlagRequired("user")
	return cmd
}

func printSessionSummary(s management.SessionStat) {
	fmt.Println(s.ID)
	fmt.Printf("  agent: %s   user: %s\n", s.AgentName, s.UserID)
	if s.DisplayName != "" {
		fmt.Printf("  %s\n", s.DisplayName)
	}
	fmt.Printf("  %d events, last updated %s\n", s.EventCount, time.Unix(s.LastUpdateTime, 0).Format(time.RFC3339))
}
