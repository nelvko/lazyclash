package cli

import (
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/spf13/cobra"
)

func newStatusCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Print installation, service and API status as JSON", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, s)
	}}
}

func newDoctorCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Diagnose service scope, systemd and API availability", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if err := writeJSON(cmd, s); err != nil {
			return err
		}
		if s.ServiceError != "" {
			return reportedError(s.ServiceError, s.ServiceDiagnostic)
		}
		if s.Capabilities.Scope == "user" && !s.Service.LingerKnown {
			fmt.Fprintln(cmd.ErrOrStderr(), commandLocale(cmd).T("cli.doctor.linger_unknown"))
		} else if s.Capabilities.Scope == "user" && !s.Service.Linger {
			fmt.Fprintln(cmd.ErrOrStderr(), commandLocale(cmd).T("cli.doctor.linger_disabled"))
		}
		if s.APIError != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), safe(commandLocale(cmd).Diagnostic(s.APIError, s.APIDiagnostic)))
		}
		if s.Service.State == "failed" {
			return diagnostic.New("diagnostic.cli.service_failed", "Mihomo service failed: %s (exit %d); inspect the systemd journal", safe(s.Service.Result), s.Service.ExitCode)
		}
		return nil
	}}
}
