package main

import (
	"fmt"
	"os"
	"strings"

	"botsonv2/core/agent"
	"botsonv2/core/management"

	"github.com/spf13/cobra"
)

// newAgentsCmd groups listing, inspecting, creating, and deleting agents.
// None of this needs the Gemini model/agent runtime bootstrapped -- same
// reasoning as `settings` and `setup`.
func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "agents",
		Short:             "List, inspect, create, or delete agents",
		PersistentPreRunE: noBootstrap,
	}
	cmd.AddCommand(newAgentsListCmd(), newAgentsShowCmd(), newAgentsToolsCmd(), newAgentsCreateCmd(), newAgentsDeleteCmd())
	return cmd
}

func newAgentsListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all agents (bundled defaults and custom user agents)",
		RunE: func(cmd *cobra.Command, args []string) error {
			details, err := management.ListAgents()
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, details)
			}
			for i, d := range details {
				if i > 0 {
					fmt.Println()
				}
				printAgentSummary(d)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newAgentsShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one agent's full config and instructions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := findAgent(args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, d)
			}
			printAgentSummary(*d)
			if d.Instructions != "" {
				fmt.Printf("\nInstructions:\n%s\n", d.Instructions)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newAgentsToolsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List tool/sub-agent names usable in `agents create --tools`",
		RunE: func(cmd *cobra.Command, args []string) error {
			toolMap, err := management.ListTools()
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, toolMap)
			}
			fmt.Println("Standard tools:")
			for _, t := range toolMap["standard"] {
				fmt.Printf("  %s\n", t)
			}
			fmt.Println("Agents available for delegation:")
			for _, a := range toolMap["agents"] {
				fmt.Printf("  %s\n", a)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newAgentsCreateCmd() *cobra.Command {
	var (
		name             string
		description      string
		toolsCSV         string
		instructions     string
		instructionsFile string
		private          bool
		asJSON           bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create or overwrite a custom agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if instructions != "" && instructionsFile != "" {
				return fmt.Errorf("pass only one of --instructions or --instructions-file")
			}
			if instructionsFile != "" {
				data, err := os.ReadFile(instructionsFile)
				if err != nil {
					return fmt.Errorf("failed to read --instructions-file: %w", err)
				}
				instructions = string(data)
			}

			var tools []string
			for _, t := range strings.Split(toolsCSV, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tools = append(tools, t)
				}
			}

			detail := agent.AgentDetail{
				AgentConfig: agent.AgentConfig{
					Name:        name,
					Description: description,
					Private:     private,
					Tools:       tools,
				},
				Instructions: instructions,
			}

			if err := management.SaveAgent(detail); err != nil {
				return err
			}

			saved, err := findAgent(name)
			if err != nil {
				// Saved fine; ListAgents failing here is a separate,
				// unlikely problem -- still report success.
				fmt.Printf("Saved agent %q.\n", name)
				return nil
			}

			if asJSON {
				return encodeJSON(cmd, saved)
			}
			fmt.Printf("Saved agent %q.\n", saved.Name)
			printAgentSummary(*saved)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Agent name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Short description of the agent's purpose")
	cmd.Flags().StringVar(&toolsCSV, "tools", "", "Comma-separated tool/sub-agent names (see `agents tools`)")
	cmd.Flags().StringVar(&instructions, "instructions", "", "Full system instructions text")
	cmd.Flags().StringVar(&instructionsFile, "instructions-file", "", "Path to a file containing the system instructions")
	cmd.Flags().BoolVar(&private, "private", false, "Hide this agent from delegation/selection lists")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the saved agent as JSON")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newAgentsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a custom user agent (bundled default agents can't be deleted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := management.DeleteAgent(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted agent %q.\n", args[0])
			return nil
		},
	}
}

func findAgent(name string) (*agent.AgentDetail, error) {
	details, err := management.ListAgents()
	if err != nil {
		return nil, err
	}
	for _, d := range details {
		if d.Name == name {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", name)
}

func printAgentSummary(d agent.AgentDetail) {
	flags := ""
	if d.IsRoot {
		flags += " [root]"
	}
	if d.ReadOnly {
		flags += " [default, read-only]"
	}
	if d.Private {
		flags += " [private]"
	}
	fmt.Printf("%s%s\n", d.Name, flags)
	if d.Description != "" {
		fmt.Printf("  %s\n", d.Description)
	}
	if len(d.Tools) > 0 {
		fmt.Printf("  tools: %s\n", strings.Join(d.Tools, ", "))
	}
}
