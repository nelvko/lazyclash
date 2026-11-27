package cli

import (
	"cmp"
	"fmt"
	"io"
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/spf13/cobra"
)

func newStatusCommand(backend func() *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "status", Short: "Show the current instance and Mihomo status", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a := backend()
		s, err := a.Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, s)
		}
		return printStatus(cmd.OutOrStdout(), commandLocale(cmd), a.Name, s)
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the instance snapshot as JSON")
	return cmd
}

func printStatus(w io.Writer, locale i18n.Localizer, name string, s app.Snapshot) error {
	tr := locale.T
	var summary strings.Builder
	row := func(key, value string) {
		fmt.Fprintf(&summary, "%s  %s\n", tr(key), safe(value))
	}
	scope := tr("cli.status.user")
	if s.Capabilities.Scope == "system" {
		scope = tr("cli.status.system")
	}
	row("cli.status.instance", scope+" · "+name+".service")
	if !s.Installed {
		row("cli.status.core", tr("cli.status.installHint"))
	} else {
		state := map[string]string{
			"active": "dashboard.status.running", "inactive": "dashboard.status.stopped",
			"activating": "dashboard.status.starting", "deactivating": "dashboard.status.stopping",
			"reloading": "dashboard.status.reloading", "failed": "dashboard.status.failed",
		}[s.Service.State]
		if s.ServiceError != "" {
			state = "dashboard.status.unavailable"
		}
		row("cli.status.core", cmp.Or(s.Version, tr("dashboard.status.unknown"))+" · "+tr(cmp.Or(state, "dashboard.status.unknown")))
		if s.RunningVersion != "" && s.RunningVersion != s.Version {
			row("cli.status.runningVersion", s.RunningVersion)
		}
		row("cli.status.proxy", cmp.Or(s.RuntimeSettings.ProxyAddress(""), tr("dashboard.status.disabled")))
		api := tr("cli.status.apiIdle")
		if s.ServiceError != "" || s.Service.Active && s.APIError != "" {
			api = tr("dashboard.status.unavailable")
		} else if s.Service.Active {
			api = tr("cli.status.available")
		}
		row("cli.status.api", api)
		if s.AppliedName != "" {
			row("cli.status.profile", s.AppliedName)
		}
		if mode := cmp.Or(s.Mode.Actual, s.Mode.Desired); mode != "" {
			row("cli.status.mode", mode)
		}
		switch s.Service.EnableState {
		case "enabled", "disabled":
			row("cli.status.autostart", tr("dashboard.status."+s.Service.EnableState))
		}
	}
	if s.ServiceError != "" || s.Service.State == "failed" || s.Service.Active && s.APIError != "" {
		fmt.Fprintln(&summary, tr("cli.status.doctorHint"))
	}
	_, err := io.WriteString(w, summary.String())
	return err
}

func newDoctorCommand(backend func() *app.App) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Diagnose service and control API issues", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := backend().Snapshot(cmd.Context())
		if err != nil {
			return err
		}
		return diagnose(cmd.OutOrStdout(), commandLocale(cmd), s)
	}}
}

func diagnose(w io.Writer, locale i18n.Localizer, s app.Snapshot) error {
	if s.ServiceError != "" {
		return reportedError(s.ServiceError, s.ServiceDiagnostic)
	}
	if s.Service.State == "failed" {
		return diagnostic.New("diagnostic.cli.service_failed", "Mihomo service failed: %s (exit %d); inspect the systemd journal", safe(s.Service.Result), s.Service.ExitCode)
	}
	if s.Service.Active && s.APIError != "" {
		return diagnostic.New("diagnostic.cli.api_unavailable", "Mihomo is running but the control API is unavailable: %s. Check the controller address and port in Settings.", safe(locale.Diagnostic(s.APIError, s.APIDiagnostic)))
	}
	if s.Capabilities.Scope == "user" && s.Installed {
		switch {
		case !s.Service.LingerKnown:
			if _, err := fmt.Fprintln(w, locale.T("cli.doctor.linger_unknown")); err != nil {
				return err
			}
		case !s.Service.Linger:
			if _, err := fmt.Fprintln(w, locale.T("cli.doctor.linger_disabled")); err != nil {
				return err
			}
		}
	}
	var result string
	switch {
	case !s.Installed:
		result = locale.T("cli.status.installHint")
	case s.Service.Active:
		result = locale.T("cli.status.healthy")
	case s.Service.State == "inactive":
		result = locale.T("cli.status.startHint")
	default:
		result = locale.T("cli.status.observe", safe(s.Service.State))
	}
	_, err := fmt.Fprintln(w, result)
	return err
}
