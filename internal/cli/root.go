// Package cli exposes automation, diagnostics and parent-shell integration.
package cli

import (
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/tui"
	"github.com/spf13/cobra"
)

func interactive(cmd *cobra.Command) bool {
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	return inOK && outOK && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

func New(args ...string) *cobra.Command {
	var language string
	var backend *app.App
	current := func() *app.App { return backend }
	initialize := func(_ *cobra.Command, _ []string) error {
		var err error
		backend, err = app.New("", "")
		return err
	}
	root := &cobra.Command{Use: "lazyclash", Short: "Manage Mihomo from a terminal UI", SilenceUsage: true, Args: noArgs}
	root.PersistentFlags().StringVar(&language, "lang", "", "Interface language: auto, en or zh-Hans")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if !interactive(cmd) {
			return usageError{diagnostic.New("diagnostic.cli.no_terminal", "TUI requires a terminal; use status, doctor or --help for automation")}
		}
		if err := initialize(cmd, args); err != nil {
			return err
		}
		runErr := tui.Run(cmd.Context(), backend, language)
		printShellActivationNotice(cmd)
		return runErr
	}
	root.AddGroup(
		&cobra.Group{ID: "service", Title: "Service"},
		&cobra.Group{ID: "core", Title: "Mihomo core"},
		&cobra.Group{ID: "shell", Title: "Shell integration"},
		&cobra.Group{ID: "help", Title: "Help and completion"},
	)
	add := func(group string, commands ...*cobra.Command) {
		for _, command := range commands {
			command.GroupID = group
			command.PreRunE = initialize
			root.AddCommand(command)
		}
	}
	for _, action := range []string{"start", "stop", "restart"} {
		add("service", serviceCommand(action, func() serviceBackend { return backend }))
	}
	add("service", newStatusCommand(current), newDoctorCommand(current))
	add("core", newInstallCommand(current), newUpgradeCommand(current), newUninstallCommand(current))
	shell := newShellCommand()
	shell.GroupID = "shell"
	env := newEnvCommand(current)
	env.PreRunE = func(cmd *cobra.Command, args []string) error {
		if unset, _ := cmd.Flags().GetBool("unset"); unset {
			return nil
		}
		return initialize(cmd, args)
	}
	shell.AddCommand(env)
	root.AddCommand(shell, newLifecycleCommand())
	configurePresentation(root, args)
	if len(args) > 0 {
		root.SetArgs(args)
	}
	return root
}
