package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

// Keep parsing and restart acknowledgement in the terminal entry point; the
// application owns all validation, reviewed state and transactional recovery.
type runtimeSettingsBackend interface {
	GetRuntimeSettings(context.Context) (app.RuntimeSettings, error)
	PrepareRuntimeSettings(context.Context, app.RuntimeSettings) (app.RuntimeSettingsPlan, error)
	ApplyRuntimeSettings(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error)
}

func newSettingsCommand(backend func() *app.App) *cobra.Command {
	return runtimeSettingsCommand(func() runtimeSettingsBackend { return backend() })
}

func runtimeSettingsCommand(backend func() runtimeSettingsBackend) *cobra.Command {
	show := func(cmd *cobra.Command, _ []string) error {
		settings, err := backend().GetRuntimeSettings(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, settings)
	}
	cmd := &cobra.Command{Use: "settings", Short: "Inspect and change managed local runtime settings", Args: cobra.NoArgs, SilenceUsage: true, RunE: show}
	cmd.AddCommand(&cobra.Command{Use: "show", Short: "Print current managed ports as JSON", Args: cobra.NoArgs, RunE: show})
	var mixed, controller int
	var dryRun, restart bool
	ports := &cobra.Command{Use: "ports", Short: "Review and apply local proxy and controller ports", Args: cobra.NoArgs}
	flags := ports.Flags()
	flags.IntVar(&mixed, "mixed-port", 0, "Local HTTP/SOCKS proxy port (1024–65535)")
	flags.IntVar(&controller, "controller-port", 0, "Local control API port (1024–65535)")
	flags.BoolVar(&dryRun, "dry-run", false, "Validate and print the proposed change without applying")
	flags.BoolVar(&restart, "restart", false, "Acknowledge restarting an active core and interrupting its connections")
	ports.RunE = func(cmd *cobra.Command, _ []string) error {
		if !flags.Changed("mixed-port") && !flags.Changed("controller-port") {
			return errors.New("specify --mixed-port or --controller-port")
		}
		a := backend()
		current, err := a.GetRuntimeSettings(cmd.Context())
		if err != nil {
			return err
		}
		proposed := current
		if flags.Changed("mixed-port") {
			proposed.MixedPort = mixed
		}
		if flags.Changed("controller-port") {
			proposed.ControllerPort = controller
		}
		plan, err := a.PrepareRuntimeSettings(cmd.Context(), proposed)
		if err != nil {
			return err
		}
		if dryRun {
			return writeJSON(cmd, plan)
		}
		if plan.Changed && plan.Active && !restart {
			if err := writeJSON(cmd, plan); err != nil {
				return err
			}
			return errors.New("the core is running; review with --dry-run, then use --restart to acknowledge interrupting its connections")
		}
		result, err := a.ApplyRuntimeSettings(cmd.Context(), plan)
		if err != nil {
			return err
		}
		if result.Changed {
			fmt.Fprintln(cmd.ErrOrStderr(), "Runtime settings applied. Existing parent-shell proxy variables require a new lazyclash env evaluation.")
		}
		return writeJSON(cmd, result)
	}
	cmd.AddCommand(ports)
	return cmd
}
