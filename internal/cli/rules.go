package cli

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/spf13/cobra"
)

func newRulesCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "rules", Short: "Print the running core's rules as JSON in matching order", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := backend().Rules(cmd.Context())
		if err != nil {
			return err
		}
		if s.State != "ready" {
			if s.Error != "" {
				return reportedError(s.Error, s.Diagnostic)
			}
			if s.State == "uninstalled" {
				return diagnostic.New("diagnostic.cli.rules_core", "Mihomo is not installed; install and start it to view effective rules")
			}
			return diagnostic.New("diagnostic.cli.rules_running", "Mihomo is not running; start it to view effective rules")
		}
		return writeJSON(cmd, s.Rules)
	}}
}
