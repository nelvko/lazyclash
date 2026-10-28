// Package cli exposes automation, diagnostics and parent-shell integration.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

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
	root.AddCommand(newInstallCommand(func() *app.App { return backend }))
	status := &cobra.Command{Use: "status", Short: "Print installation, service and API status as JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend.Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s)
	}}
	root.AddCommand(status)
	svc := &cobra.Command{Use: "service", Short: "Control the Linux systemd user service"}
	for _, action := range []string{"start", "stop", "restart"} {
		svc.AddCommand(&cobra.Command{Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " Mihomo", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return backend.Control(cmd.Context(), cmd.Name()) }})
	}
	svc.AddCommand(&cobra.Command{Use: "status", Short: "Print service status", Args: cobra.NoArgs, RunE: status.RunE})
	root.AddCommand(svc)
	profile := &cobra.Command{Use: "profile", Short: "Save and apply Mihomo YAML subscriptions"}
	profile.AddCommand(&cobra.Command{Use: "add NAME SOURCE", Short: "Validate and save a URL or absolute YAML path (never overwrites)", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error { return backend.Add(cmd.Context(), args[0], args[1]) }})
	profile.AddCommand(&cobra.Command{Use: "apply NAME", Short: "Apply a saved profile or default, with rollback on failure", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return backend.Apply(cmd.Context(), args[0]) }})
	profile.AddCommand(&cobra.Command{Use: "list", Short: "List saved profile names", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend.Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s.Profiles)
	}})
	root.AddCommand(profile)
	root.AddCommand(&cobra.Command{Use: "nodes", Short: "Print current proxy groups and nodes as JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend.Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if !s.Installed {
			return errors.New("Mihomo is not installed; run lazyclash to install")
		}
		if s.APIError != "" {
			return errors.New(s.APIError)
		}
		if s.ServiceError != "" {
			return errors.New(s.ServiceError)
		}
		return writeJSON(cmd, s.Groups)
	}})
	root.AddCommand(&cobra.Command{Use: "select GROUP NODE", Short: "Select a node in a manual selector group", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error { return backend.Select(cmd.Context(), args[0], args[1]) }})
	var unset bool
	env := &cobra.Command{Use: "env", Short: "Print POSIX shell exports; evaluate them in the parent shell", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if unset {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Unset)
			return err
		}
		port, err := backend.Port(cmd.Context())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "export http_proxy='http://127.0.0.1:%d'\nexport https_proxy='http://127.0.0.1:%d'\nexport all_proxy='socks5h://127.0.0.1:%d'\nexport HTTP_PROXY=\"$http_proxy\" HTTPS_PROXY=\"$https_proxy\" ALL_PROXY=\"$all_proxy\"\nexport no_proxy='localhost,127.0.0.1,::1' NO_PROXY='localhost,127.0.0.1,::1'\n", port, port, port)
		return err
	}}
	env.Flags().BoolVar(&unset, "unset", false, "Print unset commands even when uninstalled or configuration is damaged")
	root.AddCommand(env)
	var shell string
	init := &cobra.Command{Use: "init", Short: "Print bash/zsh helpers; does not edit shell files", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if shell != "bash" && shell != "zsh" {
			return errors.New("supported shells: bash, zsh")
		}
		prefix := "command lazyclash"
		if home != "" {
			prefix += " --home " + shellQuote(home)
		}
		if name != "" {
			prefix += " --service-name " + shellQuote(name)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), `lazyclash_on() {
  %s service start || return
  local lazyclash_exports
  lazyclash_exports=$(%s env) || return
  eval "$lazyclash_exports"
}
lazyclash_off() {
  %s
}
`, prefix, prefix, Unset)
		return err
	}}
	init.Flags().StringVar(&shell, "shell", "bash", "Shell helper syntax: bash or zsh")
	root.AddCommand(init)
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Diagnose user systemd, linger and API availability", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend.Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if err := writeJSON(cmd, s); err != nil {
			return err
		}
		if s.ServiceError != "" {
			return errors.New(s.ServiceError)
		}
		if !s.Service.Linger {
			fmt.Fprintln(cmd.ErrOrStderr(), "Linger is disabled: service lifetime may depend on your login session. To keep it running after logout, review: loginctl enable-linger \"$(id -un)\"")
		}
		if s.APIError != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), safe(s.APIError))
		}
		return nil
	}})
	return root
}

const Unset = "unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY no_proxy NO_PROXY"

func writeJSON(cmd *cobra.Command, v any) error {
	e := json.NewEncoder(cmd.OutOrStdout())
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
