package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newLifecycleCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{
		Use: "__service <prepare|ready|cleanup>", Hidden: true,
		Args:      cobra.MatchAll(exactArgs(1), onlyValidArgs),
		ValidArgs: []string{"prepare", "ready", "cleanup"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return backend().ServiceLifecycle(cmd.Context(), args[0])
		},
	}
}
