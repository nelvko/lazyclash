// Package cli exposes automation, diagnostics and parent-shell integration.
package cli

import (
	"errors"
	"os"
	"runtime"

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
		if err := requireUnprivilegedTUI(runtime.GOOS, os.Geteuid()); err != nil {
			return err
		}
		if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
			return errors.New("TUI requires a terminal; use status, doctor or --help for automation")
		}
		return tui.Run(cmd.Context(), backend)
	}
	current := func() *app.App { return backend }
	root.AddCommand(
		newInstallCommand(current),
		newUninstallCommand(current),
		newStatusCommand(current),
		newServiceCommand(current),
		newProfileCommand(current),
		newSubscriptionCommand(current),
		newNodesCommand(current),
		newSelectCommand(current),
		newEnvCommand(current),
		newInitCommand(&home, &name),
		newDoctorCommand(current),
		newStatsCommand(current),
		newConnectionsCommand(current),
		newRulesCommand(current),
		newLogsCommand(current),
		newSettingsCommand(current),
		newModeCommand(func() runtimeOptionsBackend { return current() }),
		newTUNCommand(func() runtimeOptionsBackend { return current() }),
	)
	return root
}

func requireUnprivilegedTUI(goos string, euid int) error {
	if goos == "linux" && euid == 0 {
		return errors.New("run lazyclash as your normal user without sudo; the current runtime uses an unprivileged systemd user service")
	}
	return nil
}
