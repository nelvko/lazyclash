package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newLifecycleCommand() *cobra.Command {
	var home, name string
	cmd := &cobra.Command{
		Use: "__service <prepare|ready|cleanup>", Hidden: true,
		Args:      cobra.MatchAll(exactArgs(1), onlyValidArgs),
		ValidArgs: []string{"prepare", "ready", "cleanup"},
		RunE: func(cmd *cobra.Command, args []string) error {
			backend, err := app.New(home, name)
			if err != nil {
				return err
			}
			return backend.ServiceLifecycle(cmd.Context(), args[0])
		},
	}
	cmd.Flags().StringVar(&home, "home", "", "Instance data directory for this service unit")
	cmd.Flags().StringVar(&name, "service-name", "", "Instance name for this service unit")
	return cmd
}
