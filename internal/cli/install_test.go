package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

type installService struct{ installs, actions int }

func (s *installService) Check(ctx context.Context) error     { return ctx.Err() }
func (s *installService) Available(ctx context.Context) error { return ctx.Err() }
func (s *installService) Inspect(ctx context.Context) (service.Status, error) {
	return service.Status{State: "inactive"}, ctx.Err()
}
func (s *installService) Install(ctx context.Context, _, _, _ string) error {
	s.installs++
	return ctx.Err()
}
func (s *installService) Verify(ctx context.Context, _, _, _ string) error      { return ctx.Err() }
func (s *installService) RemoveOwned(ctx context.Context, _, _, _ string) error { return ctx.Err() }
func (s *installService) Action(ctx context.Context, _ string) error            { s.actions++; return ctx.Err() }

func installBackend(t *testing.T) (*app.App, *installService) {
	t.Helper()
	a, err := app.New(t.TempDir(), "lazyclash-cli-fixture")
	if err != nil {
		t.Fatal(err)
	}
	s := &installService{}
	a.Service = s
	a.PlatformError = nil
	a.CheckPorts = func(string, int) error { return nil }
	a.Version = func(ctx context.Context, _ string) (string, error) { return "v1.20.0", ctx.Err() }
	a.Validate = func(ctx context.Context, _, _, _ string) error { return ctx.Err() }
	a.DownloadRelease = func(ctx context.Context, dest string, r mihomo.Release, cfg downloadnet.Config, p func(mihomo.Progress)) error {
		return os.WriteFile(dest, []byte("fixture core"), 0700)
	}
	return a, s
}

func TestInstallCLIUsesSharedPlanDefaultsOverridesAndExplicitReinstall(t *testing.T) {
	a, svc := installBackend(t)
	saved := downloadnet.Config{Source: "prefix", Prefix: "https://mirror.example", ProxyMode: "environment"}
	if err := a.SaveNetworkPreferences(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	queries := 0
	downloads := 0
	a.Resolve = func(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
		queries++
		if selection != "latest" || cfg.Source != "prefix" || cfg.Prefix != saved.Prefix || cfg.ProxyMode != "none" {
			t.Fatalf("selection=%s cfg=%+v", selection, cfg)
		}
		return mihomo.Release{Version: "v1.20.0"}, ctx.Err()
	}
	downloader := a.DownloadRelease
	a.DownloadRelease = func(ctx context.Context, dest string, r mihomo.Release, cfg downloadnet.Config, p func(mihomo.Progress)) error {
		downloads++
		return downloader(ctx, dest, r, cfg, p)
	}
	for _, args := range [][]string{{"--proxy-mode", "none"}, {"--proxy-mode", "none"}, {"--proxy-mode", "none", "--reinstall"}} {
		cmd := newInstallCommand(func() *app.App { return a })
		var out, diagnostics bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostics)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"InstalledVersion": "v1.20.0"`) {
			t.Fatal(out.String())
		}
	}
	if queries != 3 || downloads != 2 || svc.installs != 1 || svc.actions != 0 {
		t.Fatalf("queries=%d downloads=%d svc=%+v", queries, downloads, svc)
	}
	loaded, err := a.LoadPreferences(t.Context())
	if err != nil || loaded.Network != saved {
		t.Fatalf("override was implicitly remembered: %+v %v", loaded, err)
	}
}

func TestInstallCLILocalAndSessionCredentialsAreNotLoggedOrSaved(t *testing.T) {
	a, _ := installBackend(t)
	a.Resolve = func(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
		if cfg.ProxyURL != "http://user:private-token@127.0.0.1:18080" {
			t.Fatal("session credentials were not passed in memory")
		}
		return mihomo.Release{Version: "v1.20.0"}, nil
	}
	cmd := newInstallCommand(func() *app.App { return a })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--proxy-mode", "custom", "--proxy-url", "http://user:private-token@127.0.0.1:18080"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "private-token") {
		t.Fatal("proxy credential leaked")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "preferences.json")); !os.IsNotExist(err) {
		t.Fatal("session settings were implicitly written")
	}
	a, _ = installBackend(t)
	a.Resolve = func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
		t.Fatal("--bin queried network metadata")
		return mihomo.Release{}, nil
	}
	local := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(local, []byte("local fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd = newInstallCommand(func() *app.App { return a })
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--bin", local})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}
