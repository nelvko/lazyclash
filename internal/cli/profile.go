package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newProfileCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Save and apply Mihomo YAML subscriptions"}
	var rememberSource bool
	add := &cobra.Command{Use: "add NAME SOURCE", Short: "Validate and save a URL or absolute YAML path (never overwrites)", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if rememberSource {
			return backend().AddSubscription(cmd.Context(), args[0], args[1])
		}
		return backend().Add(cmd.Context(), args[0], args[1])
	}}
	add.Flags().BoolVar(&rememberSource, "remember-source", false, "Store the source URL/path and any URL credentials privately on this machine for explicit refresh")
	cmd.AddCommand(add)
	cmd.AddCommand(&cobra.Command{Use: "refresh NAME", Short: "Refresh a remembered subscription without applying or restarting the core", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Refresh(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "apply NAME", Short: "Apply a saved profile or default, with rollback on failure", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Apply(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List saved profile names", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s.Profiles)
	}})
	return cmd
}
