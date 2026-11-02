package cli

import (
	"fmt"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/cobra"
)

func newCoreUpdateCommand(backend func() *app.App, upgrade bool) *cobra.Command {
	name, description := "check", "Check the installed Mihomo version against official version.txt without changing files or services"
	if upgrade {
		name, description = "upgrade", "Upgrade Mihomo and restart an active owned service, rolling back on failure"
	}
	cmd := &cobra.Command{Use: name, Short: description, Args: noArgs}
	var channel string
	var yes bool
	var overrides downloadnet.Config
	flags := cmd.Flags()
	flags.StringVar(&channel, "channel", "auto", "Release channel: auto, stable or alpha")
	flags.StringVar(&overrides.Source, "download-source", "", "Release files: official, prefix, or mirror")
	flags.StringVar(&overrides.Prefix, "download-prefix", "", "Prefix prepended to the official release-file URL")
	flags.StringVar(&overrides.Mirror, "download-mirror", "", "File URL template containing {version} and {asset}")
	flags.StringVar(&overrides.ProxyMode, "proxy-mode", "", "Download connection: none, environment, or custom")
	flags.StringVar(&overrides.ProxyURL, "proxy-url", "", "Custom HTTP, HTTPS, SOCKS5, or SOCKS5H proxy URL (session-only if authenticated)")
	if upgrade {
		flags.BoolVar(&yes, "yes", false, "Confirm upgrading and restarting an active service")
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if upgrade && !yes {
			return diagnostic.New("diagnostic.cli.upgradeConfirmation", "review the target with lazyclash install check, then run lazyclash install upgrade --yes to confirm replacement and restart")
		}
		var selected mihomo.Channel
		switch channel {
		case "auto":
		case "stable":
			selected = mihomo.ChannelStable
		case "alpha":
			selected = mihomo.ChannelAlpha
		default:
			return diagnostic.New("diagnostic.cli.updateChannel", "channel must be auto, stable or alpha")
		}
		a := backend()
		prefs, err := a.ReadPreferences(cmd.Context())
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
		check, err := a.CheckUpdate(cmd.Context(), selected, cfg)
		if err != nil {
			return err
		}
		if !upgrade {
			return writeJSON(cmd, check)
		}
		locale := commandLocale(cmd)
		fmt.Fprintln(cmd.ErrOrStderr(), locale.T("cli.core_update.target", safe(check.InstalledVersion), safe(check.LatestVersion), safe(string(check.Status))))
		result, err := a.UpgradeCore(cmd.Context(), check, cfg, func(p mihomo.Progress) {
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
