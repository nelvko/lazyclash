package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newSubscriptionCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "subscription", Short: "Manage private subscription sources independently from profiles"}
	cmd.AddCommand(&cobra.Command{Use: "add NAME SOURCE", Short: "Download and remember a URL or absolute YAML path without activating it", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().AddSource(cmd.Context(), args[0], args[1])
	}})
	cmd.AddCommand(&cobra.Command{Use: "refresh NAME", Short: "Refresh a saved source; apply affected profiles separately", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().RefreshSource(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "delete NAME", Short: "Delete a subscription that no profile references", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().DeleteSource(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List source names, formats and revisions without source credentials", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		sources, err := backend().ListSources(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, sources)
	}})
	return cmd
}
