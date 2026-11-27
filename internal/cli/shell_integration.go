package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/nelvko/lazyclash/internal/shellintegration"
	"github.com/spf13/cobra"
)

func newShellCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "shell", Short: "Manage per-terminal Shell Integration", Args: noArgs, RunE: groupHelp}
	install := &cobra.Command{Use: "install", Short: "Install the calling shell's startup hook", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := shellintegration.New()
		if err != nil {
			return err
		}
		return m.InstallCurrent()
	}}
	var asJSON bool
	status := &cobra.Command{Use: "status", Short: "Show the calling shell's integration and proxy state", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := shellintegration.New()
		if err != nil {
			return err
		}
		s, err := m.Current()
		if err != nil {
			s.State, s.Detail = "error", err.Error()
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(s)
		}
		tr := commandLocale(cmd).T
		_, err = fmt.Fprintln(cmd.OutOrStdout(), tr("cli.shell.status", s.Shell, s.Installed, s.Loaded, s.HookConfirmed, s.State, s.Pending))
		if s.Detail != "" {
			fmt.Fprintln(cmd.OutOrStdout(), diagnosticText(commandLocale(cmd).Diagnostic(s.Detail, s.Diagnostic)))
		}
		if s.Pending && s.ActivationCommand != "" {
			fmt.Fprintln(cmd.OutOrStdout(), s.ActivationCommand)
		}
		return err
	}}
	status.Flags().BoolVar(&asJSON, "json", false, "Print integration status as JSON")
	uninstall := &cobra.Command{Use: "uninstall", Short: "Restore this shell and remove owned integration references", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := shellintegration.New()
		if err != nil {
			return err
		}
		id, wait, err := m.RequestDetach()
		if err != nil {
			return err
		}
		if !wait {
			return completeShellUninstall(m, id)
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		worker := exec.Command(binary, "shell", "_uninstall-finish", "--session", id)
		// A bounded worker waits independently so the parent shell can reach
		// its prompt and acknowledge restoration. It never writes to the TTY.
		if err := worker.Start(); err != nil {
			return err
		}
		_ = worker.Process.Release()
		_, err = fmt.Fprintln(cmd.OutOrStdout(), commandLocale(cmd).T("cli.shell.uninstallPending"))
		return err
	}}
	var shell, session string
	var pid int
	attach := &cobra.Command{Use: "_attach", Hidden: true, Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := shellintegration.New()
		if err != nil {
			return err
		}
		a, err := m.Attach(shell, pid, session)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n%s\n", a.SessionID, a.SessionDir, a.EndpointFile)
		return err
	}}
	attach.Flags().StringVar(&shell, "shell", "", "Calling shell type")
	attach.Flags().IntVar(&pid, "pid", 0, "Calling shell process ID")
	attach.Flags().StringVar(&session, "session", "", "Private initialized session")
	var finalizedSession string
	finalize := &cobra.Command{Use: "_finalize", Hidden: true, Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := shellintegration.New()
		if err != nil {
			return err
		}
		return m.FinalizeAttach(finalizedSession)
	}}
	finalize.Flags().StringVar(&finalizedSession, "session", "", "Session synchronized during source")
	var finishSession string
	finish := &cobra.Command{Use: "_uninstall-finish", Hidden: true, Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		m, err := shellintegration.New()
		if err != nil {
			return err
		}
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			ready, err := m.UninstallReady(finishSession)
			if err != nil {
				return err
			}
			if ready {
				return completeShellUninstall(m, finishSession)
			}
			select {
			case <-deadline.C:
				err := fmt.Errorf("uninstall pending: shell restoration was not acknowledged")
				_ = m.NoteUninstallFailure(finishSession, err)
				return err
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-tick.C:
			}
		}
	}}
	finish.Flags().StringVar(&finishSession, "session", "", "Session awaiting restoration")
	cmd.AddCommand(install, status, uninstall, attach, finalize, finish)
	return cmd
}

func completeShellUninstall(m *shellintegration.Manager, session string) error {
	if err := m.FinalizeUninstall(); err != nil {
		if session != "" {
			_ = m.NoteUninstallFailure(session, err)
		}
		return err
	}
	if session != "" {
		return m.FinishDetachedSession(session)
	}
	return nil
}

// Bubble Tea has restored the terminal before this runs, including on a
// handled interrupt. A crash cannot delete the durable pending request.
func printShellActivationNotice(cmd *cobra.Command) {
	m, err := shellintegration.New()
	if err != nil {
		return
	}
	activation, err := m.TakeNotice()
	if err != nil || activation == "" {
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), commandLocale(cmd).T("cli.shell.activationNotice", activation))
}
