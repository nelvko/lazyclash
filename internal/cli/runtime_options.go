package cli

import (
	"context"
	"os"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/spf13/cobra"
)

type runtimeOptionsBackend interface {
	GetMode(context.Context) (app.ModeStatus, error)
	SetMode(context.Context, string) error
	SetTUN(context.Context, bool) error
}

func newModeCommand(backend func() runtimeOptionsBackend) *cobra.Command {
	return &cobra.Command{Use: "mode [rule|global|direct]", Short: "Inspect or persist the Mihomo routing mode", Args: maximumArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	return tunCommand(backend, os.Geteuid)
}

func tunCommand(backend func() runtimeOptionsBackend, euid func() int) *cobra.Command {
	var scope string
	var confirmHost bool
	cmd := &cobra.Command{Use: "tun <on|off>", Short: "Control host TUN in the root system service", Long: "TUN changes traffic routing and DNS for this host, including other users and services. Enabling it requires root, --scope host and --confirm-host. The ordinary user service cannot enable TUN.", Args: cobra.MatchAll(exactArgs(1), onlyValidArgs), ValidArgs: []string{"on", "off"}, RunE: func(cmd *cobra.Command, args []string) error {
		enabled := args[0] == "on"
		if scope != "" && scope != "host" {
			return diagnostic.New("diagnostic.cli.tunScope", "TUN scope must be host")
		}
		if enabled {
			if euid() != 0 {
				return diagnostic.New("diagnostic.cli.tunRoot", "only a root-managed system instance can enable TUN; it affects traffic for the entire host")
			}
			if scope != "host" || !confirmHost {
				return diagnostic.New("diagnostic.cli.tunConfirm", "enabling TUN affects traffic routing and DNS for the entire host; pass --scope host --confirm-host to confirm")
			}
		}
		return backend().SetTUN(cmd.Context(), enabled)
	}}
	cmd.Flags().StringVar(&scope, "scope", "", "TUN traffic scope; enabling requires host")
	cmd.Flags().BoolVar(&confirmHost, "confirm-host", false, "Confirm that TUN may change routing and DNS for the entire host")
	return cmd
}
