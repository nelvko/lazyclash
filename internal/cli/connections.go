package cli

import (
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newStatsCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "stats", Short: "Sample runtime data as JSON; cumulative traffic resets with the core", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := backend().Runtime(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s)
	}}
}

func newConnectionsCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "connections", Short: "List active Mihomo connections as JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := backend().Connections(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s)
	}}
	cmd.AddCommand(&cobra.Command{Use: "close ID", Short: "Request closure of exactly one active connection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := backend().CloseConnection(cmd.Context(), args[0]); err != nil {
			return err
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Connection close requested")
		return err
	}})
	return cmd
}
