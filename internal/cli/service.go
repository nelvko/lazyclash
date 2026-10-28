package cli

import (
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newServiceCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "service", Short: "Control the Linux systemd user service"}
	for _, action := range []string{"start", "stop", "restart"} {
		cmd.AddCommand(&cobra.Command{Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " Mihomo", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return backend().Control(cmd.Context(), cmd.Name())
		}})
	}
	status := newStatusCommand(backend)
	status.Short = "Print service status"
	cmd.AddCommand(status)
	return cmd
}
