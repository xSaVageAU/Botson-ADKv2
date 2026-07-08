package main

import (
	"fmt"
	"os"

	"botsonv2/core/scripts"

	"github.com/spf13/cobra"
)

// newScriptCmd groups listing, inspecting, creating, deleting, and running
// named scripts (~/.botsonv2/scripts/<name>/main.go). None of this needs
// the Gemini model/agent runtime bootstrapped -- same reasoning as
// `settings` and `agents`.
func newScriptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "script",
		Short:             "List, inspect, create, delete, or run named scripts",
		PersistentPreRunE: noBootstrap,
	}
	cmd.AddCommand(newScriptListCmd(), newScriptShowCmd(), newScriptCreateCmd(), newScriptDeleteCmd(), newScriptRunCmd())
	return cmd
}

func newScriptListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all saved scripts",
		RunE: func(cmd *cobra.Command, args []string) error {
			details, err := scripts.List()
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, details)
			}
			if len(details) == 0 {
				fmt.Println("No scripts saved yet. Create one with `botson script create`.")
				return nil
			}
			for i, d := range details {
				if i > 0 {
					fmt.Println()
				}
				fmt.Println(d.Name)
				if d.Description != "" {
					fmt.Printf("  %s\n", d.Description)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newScriptShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one script's description and full source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := findScript(args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, d)
			}
			fmt.Println(d.Name)
			if d.Description != "" {
				fmt.Printf("  %s\n", d.Description)
			}
			fmt.Printf("\n%s\n", d.Source)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newScriptCreateCmd() *cobra.Command {
	var (
		name        string
		description string
		sourceFile  string
		asJSON      bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create or overwrite a named script from a Go source file",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(sourceFile)
			if err != nil {
				return fmt.Errorf("failed to read --source-file: %w", err)
			}

			detail := scripts.Detail{
				Name:        name,
				Description: description,
				Source:      string(data),
			}
			if err := scripts.Save(detail); err != nil {
				return err
			}

			if asJSON {
				saved, err := findScript(name)
				if err != nil {
					fmt.Printf("Saved script %q.\n", name)
					return nil
				}
				return encodeJSON(cmd, saved)
			}
			fmt.Printf("Saved script %q.\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Script name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Short description of what the script does")
	cmd.Flags().StringVar(&sourceFile, "source-file", "", "Path to a Go source file (a full 'package main' program) (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the saved script as JSON")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("source-file")
	return cmd
}

func newScriptDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a saved script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := scripts.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted script %q.\n", args[0])
			return nil
		},
	}
}

func newScriptRunCmd() *cobra.Command {
	var timeoutSeconds int

	cmd := &cobra.Command{
		Use:   "run <name> [-- args...]",
		Short: "Run a saved script by name via `go run`, passing any remaining args through to it",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			scriptArgs := args[1:]
			// SetInterspersed(false) below means Cobra never treats a
			// literal "--" specially once past the script name -- it's
			// just another positional token -- so a leading one (the
			// conventional way to mark "everything after this is for the
			// wrapped command", as with kubectl/docker exec) needs
			// stripping by hand rather than relying on Cobra to consume it.
			if len(scriptArgs) > 0 && scriptArgs[0] == "--" {
				scriptArgs = scriptArgs[1:]
			}

			result, err := scripts.Run(cmd.Context(), name, scriptArgs, timeoutSeconds)
			if err != nil {
				return err
			}

			if result.Stdout != "" {
				fmt.Print(result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Fprint(os.Stderr, result.Stderr)
			}
			if result.ExitCode != 0 {
				return fmt.Errorf("script exited with code %d", result.ExitCode)
			}
			return nil
		},
	}
	// Everything after the script name is the script's own args, even if
	// it looks like a flag -- don't let Cobra try to parse `-x`/`--foo`
	// meant for the script itself as flags of `botson script run`.
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "Maximum seconds to let the script run before it's killed (default 120)")
	return cmd
}

func findScript(name string) (*scripts.Detail, error) {
	details, err := scripts.List()
	if err != nil {
		return nil, err
	}
	for _, d := range details {
		if d.Name == name {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("script %q not found", name)
}
