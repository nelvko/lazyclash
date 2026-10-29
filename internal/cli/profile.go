package cli

import (
	"errors"
	"path/filepath"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/store"
	"github.com/spf13/cobra"
)

func newProfileCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Manage configuration recipes and apply generated runtime configurations"}
	var rememberSource bool
	add := &cobra.Command{Use: "add NAME SOURCE", Short: "Import a URL or absolute YAML path as a subscription and full profile", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if rememberSource {
			return backend().AddSubscription(cmd.Context(), args[0], args[1])
		}
		return backend().Add(cmd.Context(), args[0], args[1])
	}}
	add.Flags().BoolVar(&rememberSource, "remember-source", false, "Store the source URL/path and any URL credentials privately on this machine for explicit refresh")
	cmd.AddCommand(add)
	cmd.AddCommand(&cobra.Command{Use: "refresh NAME", Short: "Refresh a full profile's remembered subscription; apply separately", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().Refresh(cmd.Context(), args[0])
	}})
	cmd.AddCommand(newProfileCreateCommand(backend))
	cmd.AddCommand(&cobra.Command{Use: "override NAME FILE", Short: "Save a YAML set/prepend-rules/append-rules override; apply separately", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := readProfileInput(args[1])
		if err != nil {
			return err
		}
		return backend().SetProfileOverrides(cmd.Context(), args[0], raw)
	}})
	cmd.AddCommand(&cobra.Command{Use: "delete NAME", Short: "Delete an unused profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return backend().DeleteProfile(cmd.Context(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "show NAME", Short: "Show profile sources and validation state without subscription credentials", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		details, err := backend().ProfileDetails(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return writeJSON(cmd, details)
	}})
	cmd.AddCommand(&cobra.Command{Use: "apply NAME", Short: "Apply a saved profile or default, with rollback on failure", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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

func newProfileCreateCommand(backend func() *app.App) *cobra.Command {
	var source, templatePath string
	var sources []string
	var managed bool
	cmd := &cobra.Command{Use: "create NAME", Short: "Create a full profile from one subscription or a managed profile from node sources", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		spec := configuration.Profile{Core: "mihomo", Mode: "full", Source: source}
		if managed {
			if source != "" || len(sources) == 0 || templatePath == "" {
				return errors.New("managed profiles require --sources and --template, and do not accept --source")
			}
			raw, err := readProfileInput(templatePath)
			if err != nil {
				return err
			}
			spec.Mode, spec.Sources, spec.Template = "managed", sources, string(raw)
		} else if source == "" || len(sources) != 0 || templatePath != "" {
			return errors.New("full profiles require --source; use --managed with --sources and --template for node sources")
		}
		return backend().CreateProfile(cmd.Context(), args[0], spec)
	}}
	cmd.Flags().StringVar(&source, "source", "", "Subscription name for a full profile")
	cmd.Flags().BoolVar(&managed, "managed", false, "Build from a user template and subscription nodes")
	cmd.Flags().StringSliceVar(&sources, "sources", nil, "Ordered, comma-separated subscription names for a managed profile")
	cmd.Flags().StringVar(&templatePath, "template", "", "Absolute path to a managed Mihomo YAML template")
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
