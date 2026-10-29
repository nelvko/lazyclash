package cli

import (
	"context"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

type runtimeOptionsBackend interface {
	GetMode(context.Context) (app.ModeStatus, error)
	SetMode(context.Context, string) error
	SetTUN(context.Context, bool) error
}

func newModeCommand(backend func() runtimeOptionsBackend) *cobra.Command {
	return &cobra.Command{Use: "mode [rule|global|direct]", Short: "Inspect or persist the Mihomo routing mode", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a := backend()
		if len(args) == 1 {
			if err := a.SetMode(cmd.Context(), args[0]); err != nil {
				return err
			}
		}
		mode, err := a.GetMode(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, mode)
	}}
}

func newTUNCommand(backend func() runtimeOptionsBackend) *cobra.Command {
	return &cobra.Command{Use: "tun <on|off>", Short: "Request TUN state (privileged TUN is unavailable in the user service)", Args: cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs), ValidArgs: []string{"on", "off"}, RunE: func(cmd *cobra.Command, args []string) error {
		return backend().SetTUN(cmd.Context(), args[0] == "on")
	}}
}
