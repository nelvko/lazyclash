package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestCoreCheckCLIIsReadOnlyAndUsesSessionNetworkSettings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	t.Setenv("HTTPS_PROXY", "http://user:private-token@127.0.0.1:18080")
	a, err := app.New(home, "lazyclash-cli-readonly")
	if err != nil {
		t.Fatal(err)
	}
	queries := 0
	a.QueryLatest = func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (string, error) {
		queries++
		if channel != mihomo.ChannelAlpha || cfg.Method != downloadnet.MethodEnvironment {
			t.Fatal("check did not use explicit channel/environment proxy")
		}
		return "alpha-1234567", ctx.Err()
	}
	a.DownloadRelease = func(context.Context, string, mihomo.Release, downloadnet.Config, func(mihomo.Progress)) error {
		t.Fatal("version check downloaded a core")
		return nil
	}
	cmd := newInstallCommand(func() *app.App { return a })
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"check", "--channel", "alpha", "--download-method", "environment"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var check app.CoreUpdateCheck
	if err := json.Unmarshal(out.Bytes(), &check); err != nil || check.Status != mihomo.UpdateNotInstalled || check.LatestVersion != "alpha-1234567" || queries != 1 {
		t.Fatalf("check=%+v queries=%d err=%v output=%s", check, queries, err, out.String())
	}
	if strings.Contains(out.String()+stderr.String(), "private-token") || stderr.Len() != 0 {
		t.Fatal("check exposed credentials or emitted progress instead of plain JSON")
	}
	if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only check created storage or lock", err)
	}
}

func TestCoreUpgradeCLIRequiresConfirmationBeforeBackendAccess(t *testing.T) {
	cmd := newInstallCommand(func() *app.App {
		t.Fatal("unconfirmed upgrade accessed backend")
		return nil
	})
	cmd.SetArgs([]string{"upgrade"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.ExecuteContext(t.Context()); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatal("unconfirmed upgrade did not explain the explicit confirmation", err)
	}
}

func TestCoreCheckCLIReportsNetworkAndPreferenceFailures(t *testing.T) {
	a, err := app.New(t.TempDir(), "lazyclash-cli-read-error")
	if err != nil {
		t.Fatal(err)
	}
	a.QueryLatest = func(context.Context, mihomo.Channel, downloadnet.Config) (string, error) {
		return "", errors.New("fixture query timed out")
	}
	for _, corrupted := range []bool{false, true} {
		if corrupted {
			if err := os.WriteFile(filepath.Join(a.Home, "preferences.json"), []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := newInstallCommand(func() *app.App { return a })
		cmd.SilenceUsage = true
		var out, stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"check"})
		if err := cmd.ExecuteContext(t.Context()); err == nil || out.Len() != 0 {
			t.Fatal("failed query produced a successful check", err)
		}
		if _, err := os.Lstat(filepath.Join(a.Home, ".lock")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed check created a lock file")
		}
	}
}

func TestCoreUpgradeCLIUsesReviewedVersionAndSharedUpdater(t *testing.T) {
	a, svc := installBackend(t)
	a.Resolve = func(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
		return mihomo.Release{Version: "v1.20.0"}, ctx.Err()
	}
	install := newInstallCommand(func() *app.App { return a })
	var setup bytes.Buffer
	install.SetOut(&setup)
	install.SetErr(&setup)
	if err := install.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.QueryLatest = func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (string, error) {
		return "v1.21.0", ctx.Err()
	}
	a.Resolve = func(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
		if selection != "v1.21.0" {
			t.Fatal("upgrade resolved a moving latest target", selection)
		}
		return mihomo.Release{Version: selection}, ctx.Err()
	}
	a.DownloadRelease = func(ctx context.Context, dest string, release mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
		progress(mihomo.Progress{Stage: "download", Downloaded: 64, Total: 64, Detail: "fixture download"})
		return os.WriteFile(dest, []byte("version:"+release.Version), 0700)
	}
	a.Version = func(ctx context.Context, path string) (string, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if version, ok := strings.CutPrefix(string(raw), "version:"); ok {
			return version, ctx.Err()
		}
		return "v1.20.0", ctx.Err()
	}
	cmd := newInstallCommand(func() *app.App { return a })
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"upgrade", "--yes"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var upgraded app.CoreUpgradeResult
	if err := json.Unmarshal(out.Bytes(), &upgraded); err != nil || upgraded.InstalledVersion != "v1.21.0" || upgraded.PreviousVersion != "v1.20.0" || upgraded.Restarted {
		t.Fatalf("result=%+v err=%v", upgraded, err)
	}
	if svc.actions != 0 || !strings.Contains(stderr.String(), "fixture download") {
		t.Fatal("inactive service was restarted or progress was not emitted")
	}
}
