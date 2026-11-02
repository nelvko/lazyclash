package cli

import (
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/cobra"
)

func newInstallCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "install", Short: "Install, update, or explicitly reinstall Mihomo without restarting its service", Args: noArgs}
	var bin, version string
	overrides := addDownloadFlags(cmd)
	var reinstall, remember, dryRun bool
	flags := cmd.Flags()
	flags.StringVar(&bin, "bin", "", "Use an absolute local Mihomo executable path")
	flags.StringVar(&version, "core-version", "latest", "Official stable release: latest or vX.Y.Z")
	flags.BoolVar(&reinstall, "reinstall", false, "Replace the binary even when the same version is installed")
	flags.BoolVar(&dryRun, "dry-run", false, "Resolve the version and show installation preflight without installing")
	flags.BoolVar(&remember, "remember-download-settings", false, "Remember safe download settings; credentials and private URL paths are never saved")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		locale := commandLocale(cmd)
		a := backend()
		prefs, err := a.LoadPreferences(cmd.Context())
		if err != nil {
			return err
		}
		cfg, err := configuredDownload(cmd, prefs.Network, *overrides)
		if err != nil {
			return err
		}
		if bin != "" && flags.Changed("core-version") {
			return diagnostic.New("diagnostic.cli.bin_version_conflict", "--bin and --core-version cannot be combined")
		}
		if remember {
			if err := a.SaveNetworkPreferences(cmd.Context(), cfg); err != nil {
				return err
			}
		}
		var release mihomo.Release
		if bin == "" {
			fmt.Fprintln(cmd.ErrOrStderr(), locale.T("cli.install.query"))
			release, err = a.ResolveRelease(cmd.Context(), version, cfg)
			if err != nil {
				return err
			}
		}
		plan, err := a.PrepareInstall(cmd.Context(), release, bin, reinstall)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.ErrOrStderr(), locale.T("cli.install.plan", safe(plan.Release.Version), safe(plan.Operation), safe(plan.ActualPath)))
		if plan.ExistingVersion != "" {
			fmt.Fprint(cmd.ErrOrStderr(), locale.T("cli.install.versions", safe(plan.ExistingVersion), safe(plan.RunningVersion)))
		}
		if plan.DiskKnown {
			fmt.Fprint(cmd.ErrOrStderr(), locale.T("cli.install.disk", plan.FreeBytes>>20))
		}
		for _, warning := range plan.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), locale.Detail(safe(warning)))
		}
		if plan.BlockedReason != "" {
			return reportedError(plan.BlockedReason, plan.BlockedDiagnostic)
		}
		if dryRun {
			// Release metadata contains only official URLs, but keep the CLI
			// preflight deliberately independent of any session URL credentials.
			return writeJSON(cmd, struct {
				Version, Operation, Path, Installed, Running string
				FreeBytes                                    uint64
				DiskKnown                                    bool
			}{plan.Release.Version, plan.Operation, plan.ActualPath, plan.ExistingVersion, plan.RunningVersion, plan.FreeBytes, plan.DiskKnown})
		}
		result, err := a.InstallCore(cmd.Context(), plan, cfg, func(p mihomo.Progress) {
			if p.Total > 0 {
				fmt.Fprint(cmd.ErrOrStderr(), locale.T("cli.install.progress", safe(p.Stage), p.Downloaded, p.Total, p.BytesPerSecond, locale.Detail(safe(p.Detail))))
			} else {
				fmt.Fprint(cmd.ErrOrStderr(), locale.T("cli.install.progress_detail", safe(p.Stage), locale.Detail(safe(p.Detail))))
			}
		})
		if err != nil {
			return err
		}
		return writeJSON(cmd, result)
	}
	cmd.AddCommand(newCoreUpdateCommand(backend, false), newCoreUpdateCommand(backend, true))
	return cmd
}
