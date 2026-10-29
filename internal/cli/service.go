package cli

import (
	"context"
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newServiceCommand(backend func() *app.App) *cobra.Command {
	cmd := serviceCommand(func() serviceBackend { return backend() })
	status := newStatusCommand(backend)
	status.Short = "Print service status"
	cmd.AddCommand(status)
	return cmd
}

type serviceBackend interface {
	Control(context.Context, string) error
	SetAutostart(context.Context, bool) error
}

func serviceCommand(backend func() serviceBackend) *cobra.Command {
	cmd := &cobra.Command{Use: "service", Short: "Control the Linux systemd user service"}
	for _, action := range []string{"start", "stop", "restart"} {
		cmd.AddCommand(&cobra.Command{Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " Mihomo", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return backend().Control(cmd.Context(), cmd.Name())
		}})
	}
	for _, action := range []string{"enable", "disable"} {
		cmd.AddCommand(&cobra.Command{Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " automatic startup without changing the running process", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return backend().SetAutostart(cmd.Context(), cmd.Name() == "enable")
		}})
	}
	return cmd
}
