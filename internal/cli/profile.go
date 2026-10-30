package cli

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/store"
	"github.com/spf13/cobra"
)

func newProfileCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Manage subscription and local configurations"}
	var rememberSource bool
	var explicitName string
	add := &cobra.Command{Use: "add SOURCE", Short: "Import and automatically name a URL or absolute YAML path", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("name") && strings.TrimSpace(explicitName) == "" {
			return errors.New("custom name must not be empty")
		}
		if rememberSource {
			return backend().AddSubscription(cmd.Context(), explicitName, args[0])
		}
		return backend().Add(cmd.Context(), explicitName, args[0])
	}}
	add.Flags().StringVar(&explicitName, "name", "", "Use a custom display name instead of automatic naming")
	add.Flags().BoolVar(&rememberSource, "remember-source", false, "Store the source URL/path and any URL credentials privately on this machine for explicit refresh")
	cmd.AddCommand(add)
	cmd.AddCommand(&cobra.Command{Use: "rename ID NAME", Short: "Change a profile's display name while preserving its ID", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().RenameProfile(cmd.Context(), args[0], args[1])
	}})
	cmd.AddCommand(&cobra.Command{Use: "url ID SOURCE", Short: "Replace and validate a profile's remembered address", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().UpdateProfileAddress(cmd.Context(), args[0], args[1])
	}})
	cmd.AddCommand(&cobra.Command{Use: "refresh ID", Short: "Refresh a profile's remembered subscription; apply separately", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Refresh(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "override ID FILE", Short: "Save a YAML set/prepend-rules/append-rules override; apply separately", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := readProfileInput(args[1])
		if err != nil {
			return err
		}
		return backend().SetProfileOverrides(cmd.Context(), args[0], raw)
	}})
	cmd.AddCommand(&cobra.Command{Use: "delete ID", Short: "Delete an unused profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().DeleteProfile(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "show ID", Short: "Show profile origin and validation state without subscription credentials", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		details, err := backend().ProfileDetails(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return writeJSON(cmd, details)
	}})
	cmd.AddCommand(&cobra.Command{Use: "apply ID", Short: "Apply a saved profile or default, with rollback on failure", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Apply(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List profiles and validation state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		profiles, err := backend().ListProfiles(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, profiles)
	}})
	return cmd
}

func readProfileInput(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("configuration input must be an absolute file path")
	}
	raw, err := store.Read(path)
	if err != nil {
		return nil, errors.New("cannot read configuration input; use a regular file of at most 8 MiB")
	}
	if len(raw) > 8<<20 {
		return nil, errors.New("configuration input exceeds 8 MiB")
	}
	return raw, nil
}
