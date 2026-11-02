package cli

import (
	"fmt"
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/spf13/cobra"
)

const Unset = "unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY no_proxy NO_PROXY"

func newEnvCommand(backend func() *app.App) *cobra.Command {
	var unset bool
	cmd := &cobra.Command{Use: "env", Short: "Print POSIX shell exports; evaluate them in the parent shell", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if unset {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Unset)
			return err
		}
		port, err := backend().Port(cmd.Context())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "export http_proxy='http://127.0.0.1:%d'\nexport https_proxy='http://127.0.0.1:%d'\nexport all_proxy='socks5h://127.0.0.1:%d'\nexport HTTP_PROXY=\"$http_proxy\" HTTPS_PROXY=\"$https_proxy\" ALL_PROXY=\"$all_proxy\"\nexport no_proxy='localhost,127.0.0.1,::1' NO_PROXY='localhost,127.0.0.1,::1'\n", port, port, port)
		return err
	}}
	cmd.Flags().BoolVar(&unset, "unset", false, "Print unset commands even when uninstalled or configuration is damaged")
	return cmd
}

func newInitCommand(home, name *string) *cobra.Command {
	var shell string
	cmd := &cobra.Command{Use: "init", Short: "Print bash/zsh helpers; does not edit shell files", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if shell != "bash" && shell != "zsh" {
			return diagnostic.New("diagnostic.cli.shell", "supported shells: bash, zsh")
		}
		prefix := "command lazyclash"
		if *home != "" {
			prefix += " --home " + shellQuote(*home)
		}
		if *name != "" {
			prefix += " --service-name " + shellQuote(*name)
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
	cmd.Flags().StringVar(&shell, "shell", "bash", "Shell helper syntax: bash or zsh")
	return cmd
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
