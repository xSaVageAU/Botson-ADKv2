package main

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

// encodeJSON writes v to cmd's stdout as indented JSON. Shared by every
// subcommand's --json flag (settings, agents, ...) so machine callers get
// one consistent output format across the CLI.
func encodeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
