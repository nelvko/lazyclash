package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/muesli/cancelreader"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/cobra"
)

func newUpgradeCommand(backend func() *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "upgrade", Short: "Check for a Mihomo update or upgrade to the reviewed stable version"}
	var channel string
	var checkOnly, yes bool
	overrides := addDownloadFlags(cmd)
	flags := cmd.Flags()
	flags.StringVar(&channel, "channel", "auto", "Release channel: auto, stable or alpha")
	flags.BoolVar(&checkOnly, "check", false, "Only check the installed and available versions")
	flags.BoolVar(&yes, "yes", false, "Confirm upgrading and restarting an active service")
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if err := noArgs(cmd, args); err != nil {
			return err
		}
		if flags.Changed("check") && flags.Changed("yes") {
			return usageError{diagnostic.New("diagnostic.cli.upgradeFlags", "--check and --yes cannot be combined")}
		}
		if !checkOnly && !yes && !interactive(cmd) {
			return usageError{diagnostic.New("diagnostic.cli.upgradeConfirmation", "non-interactive upgrade requires --check or --yes")}
		}
		return nil
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		var selected mihomo.Channel
		switch channel {
		case "auto":
		case "stable":
			selected = mihomo.ChannelStable
		case "alpha":
			selected = mihomo.ChannelAlpha
		default:
			return usageError{diagnostic.New("diagnostic.cli.updateChannel", "channel must be auto, stable or alpha")}
		}
		a := backend()
		prefs, err := a.ReadPreferences(cmd.Context())
		if err != nil {
			return err
		}
		cfg, err := configuredDownload(cmd, prefs.Network, *overrides)
		if err != nil {
			return err
		}
		check, err := a.CheckUpdate(cmd.Context(), selected, cfg)
		if err != nil {
			return err
		}
		if checkOnly {
			return writeJSON(cmd, check)
		}
		locale := commandLocale(cmd)
		fmt.Fprintln(cmd.ErrOrStderr(), locale.T("cli.core_update.target", safe(check.InstalledVersion), safe(check.LatestVersion), safe(string(check.Status))))
		if check.Channel == mihomo.ChannelStable && check.Status == mihomo.UpdateCurrent {
			return writeJSON(cmd, app.CoreUpgradeResult{InstalledVersion: check.InstalledVersion, PreviousVersion: check.InstalledVersion})
		}
		if !yes && check.Status == mihomo.UpdateAvailable && check.Channel == mihomo.ChannelStable {
			fmt.Fprint(cmd.ErrOrStderr(), locale.T("cli.core_update.prompt"))
			confirmed, err := confirmUpgrade(cmd.Context(), cmd.InOrStdin())
			if err != nil {
				return err
			}
			if !confirmed {
				return diagnostic.New("diagnostic.cli.upgradeDeclined", "upgrade canceled")
			}
		}
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

func confirmUpgrade(ctx context.Context, in io.Reader) (bool, error) {
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return false, err
	}
	defer reader.Close()
	// Interrupt a blocked terminal read and finish cancellation before Close.
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		reader.Cancel()
		close(done)
	})
	defer func() {
		if !stop() {
			<-done
		}
	}()
	scanner := bufio.NewScanner(reader)
	read := scanner.Scan()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !read {
		return false, scanner.Err()
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}
