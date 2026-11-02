package cli

import (
	"context"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

type removalBackend interface{ RemoveCore(context.Context) error }

func newUninstallCommand(backend func() *app.App) *cobra.Command {
	return uninstallCommand(func() removalBackend { return backend() })
}

func uninstallCommand(backend func() removalBackend) *cobra.Command {
	return &cobra.Command{Use: "uninstall", Short: "Remove the stopped, disabled core and its unit; preserve saved profiles and preferences", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return backend().RemoveCore(cmd.Context())
	}}
}
