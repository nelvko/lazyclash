package cli

import (
	"errors"
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/cobra"
)

func newInstallCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "install", Short: "Install, update, or explicitly reinstall Mihomo without restarting its service", Args: cobra.NoArgs}
	var bin, version string
	var overrides downloadnet.Config
	var reinstall, remember, dryRun bool
	flags := cmd.Flags()
	flags.StringVar(&bin, "bin", "", "Use an absolute local Mihomo executable path")
	flags.StringVar(&version, "core-version", "latest", "Official stable release: latest or vX.Y.Z")
	flags.BoolVar(&reinstall, "reinstall", false, "Replace the binary even when the same version is installed")
	flags.BoolVar(&dryRun, "dry-run", false, "Resolve the version and show installation preflight without installing")
	flags.StringVar(&overrides.Source, "download-source", "", "Release files: official, prefix, or mirror")
	flags.StringVar(&overrides.Prefix, "download-prefix", "", "Prefix prepended to the official release-file URL")
	flags.StringVar(&overrides.Mirror, "download-mirror", "", "File URL template containing {version} and {asset}")
	flags.StringVar(&overrides.ProxyMode, "proxy-mode", "", "Download connection: none, environment, or custom")
	flags.StringVar(&overrides.ProxyURL, "proxy-url", "", "Custom HTTP, HTTPS, SOCKS5, or SOCKS5H proxy URL (session-only if authenticated)")
	flags.BoolVar(&remember, "remember-download-settings", false, "Remember safe download settings; credentials and private URL paths are never saved")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		a := backend()
		prefs, err := a.LoadPreferences(cmd.Context())
		if err != nil {
			return err
		}
		cfg := prefs.Network
		for flag, pair := range map[string][2]*string{
			"download-source": {&cfg.Source, &overrides.Source},
			"download-prefix": {&cfg.Prefix, &overrides.Prefix},
			"download-mirror": {&cfg.Mirror, &overrides.Mirror},
			"proxy-mode":      {&cfg.ProxyMode, &overrides.ProxyMode},
			"proxy-url":       {&cfg.ProxyURL, &overrides.ProxyURL},
		} {
			if flags.Changed(flag) {
				*pair[0] = *pair[1]
			}
		}
		cfg = cfg.Normalized()
		if err := cfg.Validate(); err != nil {
			return err
		}
		if bin != "" && flags.Changed("core-version") {
			return errors.New("--bin and --core-version cannot be combined")
		}
		if remember {
			if err := a.SaveNetworkPreferences(cmd.Context(), cfg); err != nil {
				return err
			}
		}
		var release mihomo.Release
		if bin == "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Querying the official Mihomo release metadata…")
			release, err = a.ResolveRelease(cmd.Context(), version, cfg)
			if err != nil {
				return err
			}
		}
		plan, err := a.PrepareInstall(cmd.Context(), release, bin, reinstall)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Mihomo %s · %s\nPath: %s\n", safe(plan.Release.Version), safe(plan.Operation), safe(plan.ActualPath))
		if plan.ExistingVersion != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "Installed: %s; running: %s\n", safe(plan.ExistingVersion), safe(plan.RunningVersion))
		}
		if plan.DiskKnown {
			fmt.Fprintf(cmd.ErrOrStderr(), "Free disk space: %d MiB\n", plan.FreeBytes>>20)
		}
		for _, warning := range plan.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), safe(warning))
		}
		if plan.BlockedReason != "" {
			return errors.New(plan.BlockedReason)
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
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: %d/%d bytes (%.0f bytes/s) %s\n", safe(p.Stage), p.Downloaded, p.Total, p.BytesPerSecond, safe(p.Detail))
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", safe(p.Stage), safe(p.Detail))
			}
		})
		if err != nil {
			return err
		}
		return writeJSON(cmd, result)
	}
	return cmd
}
