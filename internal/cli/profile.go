package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newProfileCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Save and apply Mihomo YAML subscriptions"}
	cmd.AddCommand(&cobra.Command{Use: "add NAME SOURCE", Short: "Validate and save a URL or absolute YAML path (never overwrites)", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Add(cmd.Context(), args[0], args[1])
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
