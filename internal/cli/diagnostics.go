package cli

import (
	"errors"
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Print installation, service and API status as JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s)
	}}
}

func newDoctorCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Diagnose user systemd, linger and API availability", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if err := writeJSON(cmd, s); err != nil {
			return err
		}
		if s.ServiceError != "" {
			return errors.New(s.ServiceError)
		}
		if !s.Service.LingerKnown {
			fmt.Fprintln(cmd.ErrOrStderr(), "Linger status is unknown: inspect loginctl show-user before relying on operation after logout.")
		} else if !s.Service.Linger {
			fmt.Fprintln(cmd.ErrOrStderr(), "Linger is disabled: service lifetime may depend on your login session. To keep it running after logout, review: loginctl enable-linger \"$(id -un)\"")
		}
		if s.APIError != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), safe(s.APIError))
		}
		if s.Service.State == "failed" {
			return fmt.Errorf("Mihomo service failed: %s (exit %d); inspect the systemd journal", safe(s.Service.Result), s.Service.ExitCode)
		}
		return nil
	}}
}
