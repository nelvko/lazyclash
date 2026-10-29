package cli

import (
	"errors"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newRulesCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "rules", Short: "Print the running core's rules as JSON in matching order", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := backend().Rules(cmd.Context())
		if err != nil {
			return err
		}
		if s.State != "ready" {
			if s.Error != "" {
				return errors.New(s.Error)
			}
			if s.State == "uninstalled" {
				return errors.New("Mihomo is not installed; install and start it to view effective rules")
			}
			return errors.New("Mihomo is not running; start it to view effective rules")
		}
		return writeJSON(cmd, s.Rules)
	}}
}
