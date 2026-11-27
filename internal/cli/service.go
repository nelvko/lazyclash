package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
)

type serviceBackend interface {
	Control(context.Context, string) error
}

func serviceCommand(action string, backend func() serviceBackend) *cobra.Command {
	return &cobra.Command{Use: action, Short: strings.ToUpper(action[:1]) + action[1:] + " Mihomo", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return backend().Control(cmd.Context(), action)
	}}
}
