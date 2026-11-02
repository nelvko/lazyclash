package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/spf13/cobra"
)

func newNodesCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "nodes", Short: "Print current proxy groups and nodes as JSON", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if !s.Installed {
			return diagnostic.New("diagnostic.cli.core_required", "Mihomo is not installed; run lazyclash to install")
		}
		if s.APIError != "" {
			return reportedError(s.APIError, s.APIDiagnostic)
		}
		if s.ServiceError != "" {
			return reportedError(s.ServiceError, s.ServiceDiagnostic)
		}
		return writeJSON(cmd, s.Groups)
	}}
}

func newSelectCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "select GROUP NODE", Short: "Select a node in a manual selector group", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Select(cmd.Context(), args[0], args[1])
	}}
}
