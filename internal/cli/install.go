package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/cobra"
)

func newInstallCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "install", Short: "Install, update, or explicitly reinstall Mihomo without restarting its service"}
	var bin, version string
	overrides := addDownloadFlags(cmd)
	var reinstall, dryRun bool
	flags := cmd.Flags()
	flags.StringVar(&bin, "bin", "", "Use an absolute local Mihomo executable path")
	flags.StringVar(&version, "core-version", "latest", "Official stable release: latest or vX.Y.Z")
	flags.BoolVar(&reinstall, "reinstall", false, "Replace the binary even when the same version is installed")
	flags.BoolVar(&dryRun, "dry-run", false, "Resolve the version and show installation preflight without installing")
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if err := noArgs(cmd, args); err != nil {
			return err
		}
		if flags.Changed("bin") && !filepath.IsAbs(bin) {
			return usageError{diagnostic.New("diagnostic.install.localAbsolute", "local Mihomo path must be absolute")}
		}
		if bin != "" && flags.Changed("core-version") {
			return usageError{diagnostic.New("diagnostic.cli.bin_version_conflict", "--bin and --core-version cannot be combined")}
		}
		return nil
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		locale := commandLocale(cmd)
		a := backend()
		prefs, err := a.ReadPreferences(cmd.Context())
		if err != nil {
			return err
		}
		cfg, err := configuredDownload(cmd, prefs.Network, *overrides)
		if err != nil {
			return err
		}
		var release mihomo.Release
		if bin == "" {
			fmt.Fprintln(cmd.ErrOrStderr(), locale.T("cli.install.query"))
			release, err = a.ResolveRelease(cmd.Context(), version, cfg)
			if err != nil {
				var d *diagnostic.Error
				if flags.Changed("core-version") && errors.As(err, &d) && d.Code == "diagnostic.release.versionChoice" && version != "latest" && version != "" {
					return usageError{err}
				}
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
			// Resolved releases contain only official URLs, but keep the CLI
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
	return cmd
}
