//go:build !windows

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTrayCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "tray",
		Short:             "Show a system tray icon to monitor and control background services (Windows only)",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("the tray icon is currently only supported on Windows")
		},
	}
}
