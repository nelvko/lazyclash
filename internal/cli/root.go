// Package cli exposes automation, diagnostics and parent-shell integration.
package cli

import (
	"errors"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/tui"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	var home, name string
	var backend *app.App
	root := &cobra.Command{Use: "lazyclash", Short: "Manage Mihomo from a terminal UI", SilenceUsage: true, Args: cobra.NoArgs}
	root.PersistentFlags().StringVar(&home, "home", "", "Private lazyclash data directory (or LAZYCLASH_HOME)")
	root.PersistentFlags().StringVar(&name, "service-name", "", "Systemd user service name (or LAZYCLASH_SERVICE)")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "env" {
			if unset, _ := cmd.Flags().GetBool("unset"); unset {
				return nil
			}
		}
		var err error
		backend, err = app.New(home, name)
		return err
	}
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
			return errors.New("TUI requires a terminal; use status, doctor or --help for automation")
		}
		return tui.Run(cmd.Context(), backend)
	}
	current := func() *app.App { return backend }
	root.AddCommand(
		newInstallCommand(current),
		newStatusCommand(current),
		newServiceCommand(current),
		newProfileCommand(current),
		newNodesCommand(current),
		newSelectCommand(current),
		newEnvCommand(current),
		newInitCommand(&home, &name),
		newDoctorCommand(current),
	)
	return root
}
