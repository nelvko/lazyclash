package cli

import (
	"errors"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newNodesCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "nodes", Short: "Print current proxy groups and nodes as JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if !s.Installed {
			return errors.New("Mihomo is not installed; run lazyclash to install")
		}
		if s.APIError != "" {
			return errors.New(s.APIError)
		}
		if s.ServiceError != "" {
			return errors.New(s.ServiceError)
		}
		return writeJSON(cmd, s.Groups)
	}}
}

func newSelectCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "select GROUP NODE", Short: "Select a node in a manual selector group", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Select(cmd.Context(), args[0], args[1])
	}}
}
